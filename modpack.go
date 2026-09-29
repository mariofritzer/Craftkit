package main

// Importing modpacks: Modrinth (.mrpack) and CurseForge (.zip with manifest.json).

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ModpackRef struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Source    string `json:"source,omitempty"`
	ProjectID string `json:"projectId,omitempty"`
	VersionID string `json:"versionId,omitempty"`
	IconURL   string `json:"iconUrl,omitempty"`
}

type ModpackResult struct {
	Instance *Instance       `json:"instance"`
	Manual   []ManualFile    `json:"manual"`
	Failed   []string        `json:"failed"`
	Skipped  int             `json:"skipped"` // server-only files
	Files    int             `json:"files"`
	Identify *IdentifyResult `json:"identify,omitempty"`
}

type ManualFile struct {
	Name     string `json:"name"`
	FileName string `json:"fileName"`
	URL      string `json:"url"`
	Folder   string `json:"folder"`
}

// safeJoin joins a relative path from an archive to base and refuses anything that escapes base.
func safeJoin(base, rel string) (string, error) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return "", errf("ungültiger Pfad %q", rel)
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errf("ungültiger Pfad %q", rel)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", errf("ungültiger Pfad %q", rel)
		}
	}
	return filepath.Join(base, filepath.FromSlash(clean)), nil
}

type mrpackIndex struct {
	FormatVersion int    `json:"formatVersion"`
	Game          string `json:"game"`
	VersionID     string `json:"versionId"`
	Name          string `json:"name"`
	Summary       string `json:"summary"`
	Files         []struct {
		Path      string            `json:"path"`
		Hashes    map[string]string `json:"hashes"`
		Env       map[string]string `json:"env"`
		Downloads []string          `json:"downloads"`
		FileSize  int64             `json:"fileSize"`
	} `json:"files"`
	Dependencies map[string]string `json:"dependencies"`
}

type cfManifest struct {
	Minecraft struct {
		Version    string `json:"version"`
		ModLoaders []struct {
			ID      string `json:"id"`
			Primary bool   `json:"primary"`
		} `json:"modLoaders"`
	} `json:"minecraft"`
	ManifestType string `json:"manifestType"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Author       string `json:"author"`
	Files        []struct {
		ProjectID int  `json:"projectID"`
		FileID    int  `json:"fileID"`
		Required  bool `json:"required"`
	} `json:"files"`
	Overrides string `json:"overrides"`
}

// forgeFullVersion turns "47.2.0" into the maven version "1.20.1-47.2.0" used by the installer.
func forgeFullVersion(mc, short string) string {
	if all, err := forgeAllVersions(); err == nil {
		for _, v := range all {
			if forgeMC(v) == mc && forgeShort(v) == short {
				return v
			}
		}
	}
	return mc + "-" + short
}

func createPackInstance(j *Job, ref ModpackRef, nameOverride, mc, loader, lv string) (*Instance, *ModpackResult, error) {
	name := strings.TrimSpace(nameOverride)
	if name == "" {
		name = strings.TrimSpace(ref.Name)
		if ref.Version != "" && !strings.Contains(name, ref.Version) {
			name += " " + ref.Version
		}
	}
	if name == "" {
		name = "Modpack " + mc
	}
	j.logf("Modpack „%s“: %s %s, Minecraft %s", ref.Name, loaderNames[loader], lv, mc)
	in, err := createInstance(j, CreateInstanceReq{Name: name, MCVersion: mc, Loader: loader, LoaderVersion: lv, MemoryGB: 6})
	if err != nil {
		return nil, nil, err
	}
	instMu.Lock()
	in.Modpack = &ref
	err = in.save()
	instMu.Unlock()
	return in, &ModpackResult{Instance: in}, err
}

func finishPack(j *Job, in *Instance, res *ModpackResult) (*ModpackResult, error) {
	if in.Loader != "vanilla" {
		j.setStep("Erkenne die Mods (für Updates und Abhängigkeiten) …", 0.9)
		if t, err := loadTarget("instance", in.ID); err == nil {
			if r, err := identifyTarget(j, t); err == nil {
				res.Identify = r
			} else {
				j.logf("Mods konnten nicht online erkannt werden: %v", err)
			}
		}
	}
	j.logf("Modpack installiert: %d Dateien%s.", res.Files-len(res.Failed)-len(res.Manual),
		map[bool]string{true: sprintf(", %d nur für Server übersprungen", res.Skipped), false: ""}[res.Skipped > 0])
	if nin, err := loadInstance(in.ID); err == nil {
		res.Instance = nin
	}
	if len(res.Failed) > 0 && res.Files > 0 && len(res.Failed) == res.Files {
		return res, errNew("keine Datei des Modpacks konnte geladen werden")
	}
	return res, nil
}

func runParallel(n, workers int, fn func(i int)) {
	var wg sync.WaitGroup
	ch := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
}

// installModpackFromSource downloads the newest (or a given) version of a modpack project and imports it.
func installModpackFromSource(j *Job, source, projectID, versionID, nameOverride string) (*ModpackResult, error) {
	p, err := providerFor(source)
	if err != nil {
		return nil, err
	}
	proj, err := p.Project(projectID)
	if err != nil {
		return nil, err
	}
	var v *ModVersion
	if versionID != "" {
		v, err = p.Version(projectID, versionID)
		if err != nil {
			return nil, err
		}
	} else {
		vs, err := p.Versions(projectID, "modpack", "", nil)
		if err != nil {
			return nil, err
		}
		v = pickBest(vs)
	}
	if v == nil || v.File == nil {
		return nil, errf("keine Version von „%s“ gefunden", proj.Name)
	}
	if v.File.URL == "" {
		return nil, errf("der Autor erlaubt keine Downloads über andere Programme – lade das Modpack auf %s herunter und importiere die Datei", v.File.ManualURL)
	}
	j.logf("Lade Modpack %s %s …", proj.Name, v.Number)
	tmp := filepath.Join(dataDir(), "tmp", fmt.Sprintf("modpack-%d.zip", time.Now().UnixNano()))
	if err := download(v.File.URL, tmp, v.File.Hash, func(done, total int64) {
		if total > 0 {
			j.setStep(sprintf("Lade Modpack … %d / %d MB", done>>20, total>>20), 0.1*float64(done)/float64(total))
		}
	}); err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	return importModpack(j, tmp, ModpackRef{Name: proj.Name, Version: v.Number, Source: source, ProjectID: projectID, VersionID: v.ID, IconURL: proj.IconURL}, nameOverride)
}
