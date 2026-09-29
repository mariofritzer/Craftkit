package main

// Importing modpacks: Modrinth (.mrpack) and CurseForge (.zip with manifest.json).

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
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
		return "", fmt.Errorf("ungültiger Pfad %q", rel)
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("ungültiger Pfad %q", rel)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", fmt.Errorf("ungültiger Pfad %q", rel)
		}
	}
	return filepath.Join(base, filepath.FromSlash(clean)), nil
}

// extractPrefix copies all zip entries below prefix into dest.
func extractPrefix(zr *zip.Reader, prefix, dest string) (int, error) {
	n := 0
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) || f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		if rel == "" {
			continue
		}
		target, err := safeJoin(dest, rel)
		if err != nil {
			return n, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return n, err
		}
		rc, err := f.Open()
		if err != nil {
			return n, err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return n, err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 2<<30))
		rc.Close()
		out.Close()
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
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

// importModpack creates an instance from a modpack file.
func importModpack(j *Job, file string, ref ModpackRef, nameOverride string) (*ModpackResult, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, fmt.Errorf("Datei ist kein gültiges Modpack (kein Zip): %w", err)
	}
	defer zr.Close()
	if b := zipFile(&zr.Reader, "modrinth.index.json"); b != nil {
		return importMrpack(j, &zr.Reader, b, ref, nameOverride)
	}
	if b := zipFile(&zr.Reader, "manifest.json"); b != nil {
		return importCurseForgePack(j, &zr.Reader, b, ref, nameOverride)
	}
	return nil, errors.New("unbekanntes Modpack-Format – unterstützt werden Modrinth (.mrpack) und CurseForge (.zip mit manifest.json)")
}

func importMrpack(j *Job, zr *zip.Reader, raw []byte, ref ModpackRef, nameOverride string) (*ModpackResult, error) {
	var idx mrpackIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, fmt.Errorf("modrinth.index.json nicht lesbar: %w", err)
	}
	if idx.Game != "" && idx.Game != "minecraft" {
		return nil, fmt.Errorf("Modpack ist nicht für Minecraft")
	}
	mc := idx.Dependencies["minecraft"]
	if mc == "" {
		return nil, errors.New("Modpack nennt keine Minecraft-Version")
	}
	loader, lv := "vanilla", ""
	switch {
	case idx.Dependencies["neoforge"] != "":
		loader, lv = "neoforge", idx.Dependencies["neoforge"]
	case idx.Dependencies["forge"] != "":
		loader, lv = "forge", forgeFullVersion(mc, idx.Dependencies["forge"])
	case idx.Dependencies["quilt-loader"] != "":
		loader, lv = "quilt", idx.Dependencies["quilt-loader"]
	case idx.Dependencies["fabric-loader"] != "":
		loader, lv = "fabric", idx.Dependencies["fabric-loader"]
	}
	if ref.Name == "" {
		ref.Name = idx.Name
	}
	if ref.Version == "" {
		ref.Version = idx.VersionID
	}
	in, res, err := createPackInstance(j, ref, nameOverride, mc, loader, lv)
	if err != nil {
		return nil, err
	}

	type task struct {
		dest string
		urls []string
		hash *Hash
		name string
	}
	var tasks []task
	for _, f := range idx.Files {
		if f.Env["client"] == "unsupported" {
			res.Skipped++
			continue
		}
		dest, err := safeJoin(in.Dir, f.Path)
		if err != nil {
			return nil, err
		}
		var h *Hash
		if v := f.Hashes["sha512"]; v != "" {
			h = &Hash{"sha512", v}
		} else if v := f.Hashes["sha1"]; v != "" {
			h = &Hash{"sha1", v}
		}
		var urls []string
		for _, u := range f.Downloads {
			if strings.HasPrefix(u, "https://") {
				urls = append(urls, u)
			}
		}
		tasks = append(tasks, task{dest, urls, h, path.Base(f.Path)})
	}
	res.Files = len(tasks)
	var mu sync.Mutex
	done := 0
	runParallel(len(tasks), 6, func(i int) {
		t := tasks[i]
		var lastErr error = errors.New("keine Download-Adresse")
		for _, u := range t.urls {
			if lastErr = download(u, t.dest, t.hash, nil); lastErr == nil {
				break
			}
		}
		mu.Lock()
		done++
		j.setStep(fmt.Sprintf("Lade Dateien des Modpacks … %d/%d", done, len(tasks)), 0.1+0.7*float64(done)/float64(len(tasks)))
		if lastErr != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", t.name, lastErr))
			j.logf("✗ %s: %v", t.name, lastErr)
		}
		mu.Unlock()
	})
	j.setStep("Kopiere Einstellungen des Modpacks …", 0.85)
	for _, p := range []string{"overrides/", "client-overrides/"} {
		n, err := extractPrefix(zr, p, in.Dir)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			j.logf("%d Datei(en) aus %s übernommen.", n, strings.TrimSuffix(p, "/"))
		}
	}
	return finishPack(j, in, res)
}

func importCurseForgePack(j *Job, zr *zip.Reader, raw []byte, ref ModpackRef, nameOverride string) (*ModpackResult, error) {
	var m cfManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest.json nicht lesbar: %w", err)
	}
	mc := m.Minecraft.Version
	if mc == "" {
		return nil, errors.New("Modpack nennt keine Minecraft-Version")
	}
	loader, lv := "vanilla", ""
	for _, l := range m.Minecraft.ModLoaders {
		if !l.Primary && len(m.Minecraft.ModLoaders) > 1 {
			continue
		}
		id := l.ID
		switch {
		case strings.HasPrefix(id, "neoforge-"):
			loader, lv = "neoforge", strings.TrimPrefix(id, "neoforge-")
			if strings.HasPrefix(lv, mc+"-") { // 1.20.1 NeoForge uses forge-style versions
				loader, lv = "forge", forgeFullVersion(mc, strings.TrimPrefix(lv, mc+"-"))
			}
		case strings.HasPrefix(id, "forge-"):
			loader, lv = "forge", forgeFullVersion(mc, strings.TrimPrefix(id, "forge-"))
		case strings.HasPrefix(id, "fabric-"):
			loader, lv = "fabric", strings.TrimPrefix(id, "fabric-")
		case strings.HasPrefix(id, "quilt-"):
			loader, lv = "quilt", strings.TrimPrefix(id, "quilt-")
		}
	}
	p, err := providerFor("curseforge")
	if err != nil {
		return nil, fmt.Errorf("CurseForge-Modpacks brauchen einen CurseForge-API-Key (Einstellungen)")
	}
	cf := p.(curseforge)
	if ref.Name == "" {
		ref.Name = m.Name
	}
	if ref.Version == "" {
		ref.Version = m.Version
	}
	in, res, err := createPackInstance(j, ref, nameOverride, mc, loader, lv)
	if err != nil {
		return nil, err
	}

	// resolve all files and their projects in batches
	j.setStep("Frage die Dateien bei CurseForge ab …", 0.1)
	var fileIDs, modIDs []int
	for _, f := range m.Files {
		fileIDs = append(fileIDs, f.FileID)
		modIDs = append(modIDs, f.ProjectID)
	}
	files := map[int]cfFile{}
	for i := 0; i < len(fileIDs); i += 500 {
		end := min(i+500, len(fileIDs))
		var r struct {
			Data []cfFile `json:"data"`
		}
		if err := postJSON(curseforgeAPI+"/mods/files", cf.headers(), map[string]any{"fileIds": fileIDs[i:end]}, &r); err != nil {
			return nil, cfErr(err)
		}
		for _, f := range r.Data {
			files[f.ID] = f
		}
	}
	mods := map[int]cfMod{}
	for i := 0; i < len(modIDs); i += 500 {
		end := min(i+500, len(modIDs))
		var r struct {
			Data []cfMod `json:"data"`
		}
		if err := postJSON(curseforgeAPI+"/mods", cf.headers(), map[string]any{"modIds": modIDs[i:end]}, &r); err != nil {
			return nil, cfErr(err)
		}
		for _, md := range r.Data {
			mods[md.ID] = md
		}
	}
	folderFor := func(classID int) string {
		switch classID {
		case 12:
			return "resourcepacks"
		case 6552:
			return "shaderpacks"
		case 6945:
			return "datapacks-craftkit" // data packs belong into a world; keep them aside
		}
		return "mods"
	}
	type task struct {
		dest, url, name string
		hash            *Hash
	}
	var tasks []task
	for _, f := range m.Files {
		file, ok := files[f.FileID]
		md := mods[f.ProjectID]
		name := md.Name
		if name == "" {
			name = "Projekt " + strconv.Itoa(f.ProjectID)
		}
		if !ok {
			res.Failed = append(res.Failed, name+": Datei nicht gefunden")
			continue
		}
		folder := folderFor(md.ClassID)
		fname := safeFileName(file.FileName)
		if file.DownloadURL == "" {
			page := md.Links.WebsiteURL
			if page == "" {
				page = "https://www.curseforge.com/projects/" + strconv.Itoa(f.ProjectID)
			}
			res.Manual = append(res.Manual, ManualFile{Name: name, FileName: fname, URL: page + "/files/" + strconv.Itoa(file.ID), Folder: filepath.Join(in.Dir, folder)})
			continue
		}
		var h *Hash
		for _, x := range file.Hashes {
			if x.Algo == 1 {
				h = &Hash{"sha1", x.Value}
			}
		}
		tasks = append(tasks, task{filepath.Join(in.Dir, folder, fname), file.DownloadURL, name, h})
	}
	res.Files = len(tasks) + len(res.Manual)
	var mu sync.Mutex
	done := 0
	runParallel(len(tasks), 6, func(i int) {
		t := tasks[i]
		err := download(t.url, t.dest, t.hash, nil)
		mu.Lock()
		done++
		j.setStep(fmt.Sprintf("Lade Dateien des Modpacks … %d/%d", done, len(tasks)), 0.15+0.65*float64(done)/float64(max(1, len(tasks))))
		if err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", t.name, err))
			j.logf("✗ %s: %v", t.name, err)
		}
		mu.Unlock()
	})
	over := m.Overrides
	if over == "" {
		over = "overrides"
	}
	j.setStep("Kopiere Einstellungen des Modpacks …", 0.85)
	if n, err := extractPrefix(zr, strings.TrimSuffix(over, "/")+"/", in.Dir); err != nil {
		return nil, err
	} else if n > 0 {
		j.logf("%d Datei(en) aus %s übernommen.", n, over)
	}
	for _, mf := range res.Manual {
		j.logf("⚠ %s muss manuell geladen werden: %s", mf.Name, mf.URL)
	}
	return finishPack(j, in, res)
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
		map[bool]string{true: fmt.Sprintf(", %d nur für Server übersprungen", res.Skipped), false: ""}[res.Skipped > 0])
	if nin, err := loadInstance(in.ID); err == nil {
		res.Instance = nin
	}
	if len(res.Failed) > 0 && res.Files > 0 && len(res.Failed) == res.Files {
		return res, errors.New("keine Datei des Modpacks konnte geladen werden")
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
		return nil, fmt.Errorf("keine Version von „%s“ gefunden", proj.Name)
	}
	if v.File.URL == "" {
		return nil, fmt.Errorf("der Autor erlaubt keine Downloads über andere Programme – lade das Modpack auf %s herunter und importiere die Datei", v.File.ManualURL)
	}
	j.logf("Lade Modpack %s %s …", proj.Name, v.Number)
	tmp := filepath.Join(dataDir(), "tmp", fmt.Sprintf("modpack-%d.zip", time.Now().UnixNano()))
	if err := download(v.File.URL, tmp, v.File.Hash, func(done, total int64) {
		if total > 0 {
			j.setStep(fmt.Sprintf("Lade Modpack … %d / %d MB", done>>20, total>>20), 0.1*float64(done)/float64(total))
		}
	}); err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	return importModpack(j, tmp, ModpackRef{Name: proj.Name, Version: v.Number, Source: source, ProjectID: projectID, VersionID: v.ID, IconURL: proj.IconURL}, nameOverride)
}
