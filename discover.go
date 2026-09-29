package main

// Discovering what is already installed: launcher profiles, versions and mods
// that were not installed by CraftKit.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type FoundProfile struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	VersionID     string `json:"versionId"`
	Loader        string `json:"loader"` // vanilla, fabric, quilt, forge, neoforge, optifine, unknown
	LoaderVersion string `json:"loaderVersion"`
	MCVersion     string `json:"mcVersion"`
	GameDir       string `json:"gameDir"`
	ModCount      int    `json:"modCount"`
	LastUsed      string `json:"lastUsed"`
	InstanceID    string `json:"instanceId,omitempty"` // managed by CraftKit
	Installed     bool   `json:"installed"`            // version files exist
}

type InstalledVersion struct {
	ID            string `json:"id"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion"`
	MCVersion     string `json:"mcVersion"`
}

// detectVersion works out loader and Minecraft version from a launcher version id.
func detectVersion(mcDir, id string) InstalledVersion {
	v := InstalledVersion{ID: id, Loader: "vanilla", MCVersion: id}
	low := strings.ToLower(id)
	switch {
	case id == "latest-release" || id == "latest-snapshot":
		v.MCVersion = ""
		var m mojangManifest
		if getJSON(mojangManifestURL, nil, 30*time.Minute, &m) == nil {
			if id == "latest-release" {
				v.MCVersion = m.Latest.Release
			} else {
				v.MCVersion = m.Latest.Snapshot
			}
		}
		return v
	case strings.HasPrefix(low, "fabric-loader-"), strings.HasPrefix(low, "quilt-loader-"):
		v.Loader = strings.SplitN(low, "-", 2)[0]
		rest := id[len(v.Loader)+len("-loader-"):]
		// <loaderVersion>-<mc>; loader versions have no '-' except betas, so split at the last '-' that starts an MC version
		if i := strings.LastIndex(rest, "-"); i > 0 {
			v.LoaderVersion, v.MCVersion = rest[:i], rest[i+1:]
			// "0.27.0-beta.1-1.21" -> loader "0.27.0-beta.1"
		}
	case strings.HasPrefix(low, "neoforge-"):
		v.Loader = "neoforge"
		v.LoaderVersion = id[len("neoforge-"):]
		v.MCVersion = neoforgeMC(v.LoaderVersion)
	case strings.Contains(low, "forge"):
		v.Loader = "forge"
		i := strings.Index(low, "forge")
		v.MCVersion = strings.TrimRight(id[:i], "-")
		v.LoaderVersion = strings.TrimLeft(id[i+len("forge"):], "-")
		if j := strings.Index(v.LoaderVersion, "-"); j > 0 && strings.HasPrefix(v.LoaderVersion[j+1:], v.MCVersion) {
			v.LoaderVersion = v.LoaderVersion[:j] // 1.7.10-Forge10.13.4.1614-1.7.10
		}
	case strings.Contains(low, "optifine"):
		v.Loader = "optifine"
		v.MCVersion = strings.SplitN(id, "-", 2)[0]
	}
	// the version json knows best which Minecraft version it builds on
	if b, err := os.ReadFile(filepath.Join(mcDir, "versions", id, id+".json")); err == nil {
		var j struct {
			InheritsFrom string `json:"inheritsFrom"`
			ID           string `json:"id"`
		}
		if json.Unmarshal(b, &j) == nil && j.InheritsFrom != "" {
			v.MCVersion = j.InheritsFrom
		}
	}
	return v
}

func installedVersions() []InstalledVersion {
	mcDir := getConfig().MinecraftDir
	var out []InstalledVersion
	for id := range listVersionDirs(mcDir) {
		out = append(out, detectVersion(mcDir, id))
	}
	sort.Slice(out, func(i, k int) bool { return out[i].ID < out[k].ID })
	return out
}

func countJars(dir string) int {
	ents, _ := os.ReadDir(dir)
	n := 0
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			n++
		}
	}
	return n
}

// instanceIDForDir returns the CraftKit instance using that folder, if any.
func instanceIDForDir(dir string) string {
	if !fileExists(filepath.Join(dir, instanceManifest)) {
		return ""
	}
	c := getConfig()
	for id, d := range c.LinkedInstances {
		if samePath(d, dir) {
			return id
		}
	}
	if samePath(filepath.Dir(dir), c.InstancesDir) {
		return filepath.Base(dir)
	}
	return ""
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

// discoverProfiles reads all profiles of the official launcher.
func discoverProfiles() []FoundProfile {
	mcDir := getConfig().MinecraftDir
	seen := map[string]bool{}
	var out []FoundProfile
	installed := listVersionDirs(mcDir)
	for _, n := range []string{"launcher_profiles.json", "launcher_profiles_microsoft_store.json"} {
		b, err := os.ReadFile(filepath.Join(mcDir, n))
		if err != nil {
			continue
		}
		var root struct {
			Profiles map[string]struct {
				Name          string `json:"name"`
				Type          string `json:"type"`
				LastVersionID string `json:"lastVersionId"`
				GameDir       string `json:"gameDir"`
				LastUsed      string `json:"lastUsed"`
			} `json:"profiles"`
		}
		if json.Unmarshal(b, &root) != nil {
			continue
		}
		for key, p := range root.Profiles {
			if seen[key] {
				continue
			}
			seen[key] = true
			fp := FoundProfile{Key: key, Name: p.Name, VersionID: p.LastVersionID, GameDir: p.GameDir, LastUsed: p.LastUsed}
			if fp.GameDir == "" {
				fp.GameDir = mcDir
			}
			if fp.Name == "" {
				switch p.Type {
				case "latest-release":
					fp.Name = "Neueste Version"
				case "latest-snapshot":
					fp.Name = "Neuester Snapshot"
				default:
					fp.Name = key
				}
			}
			if fp.VersionID == "" {
				fp.VersionID = map[string]string{"latest-snapshot": "latest-snapshot"}[p.Type]
				if fp.VersionID == "" {
					fp.VersionID = "latest-release"
				}
			}
			v := detectVersion(mcDir, fp.VersionID)
			fp.Loader, fp.LoaderVersion, fp.MCVersion = v.Loader, v.LoaderVersion, v.MCVersion
			fp.Installed = installed[fp.VersionID] || strings.HasPrefix(fp.VersionID, "latest-")
			fp.ModCount = countJars(filepath.Join(fp.GameDir, "mods"))
			fp.InstanceID = instanceIDForDir(fp.GameDir)
			if strings.HasPrefix(key, "craftkit-") && fp.InstanceID == "" {
				fp.InstanceID = strings.TrimPrefix(key, "craftkit-")
			}
			out = append(out, fp)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].LastUsed > out[k].LastUsed })
	return out
}

// adoptProfile makes an existing launcher profile a CraftKit instance.
func adoptProfile(j *Job, key string) (*Instance, error) {
	var fp *FoundProfile
	for _, p := range discoverProfiles() {
		if p.Key == key {
			p := p
			fp = &p
		}
	}
	if fp == nil {
		return nil, fmt.Errorf("Profil nicht gefunden")
	}
	if fp.InstanceID != "" {
		if in, err := loadInstance(fp.InstanceID); err == nil {
			return in, nil
		}
	}
	loader := fp.Loader
	if loader == "optifine" || loader == "unknown" {
		loader = "vanilla"
	}
	// OptiFine or unknown profile with mods: guess the loader from the jars
	if loader == "vanilla" && fp.ModCount > 0 {
		counts := map[string]int{}
		for _, ji := range scanFolder(filepath.Join(fp.GameDir, "mods")) {
			counts[ji.Loader]++
		}
		best := 0
		for _, l := range []string{"fabric", "forge", "neoforge", "quilt"} {
			if counts[l] > best {
				best, loader = counts[l], l
			}
		}
	}
	base := slugify(fp.Name)
	id := base
	for n := 2; instanceIDExists(id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	in := &Instance{ID: id, Name: fp.Name, MCVersion: fp.MCVersion, Loader: loader, LoaderVersion: fp.LoaderVersion,
		VersionID: fp.VersionID, Created: time.Now().Format(time.RFC3339), Items: map[string]*InstalledItem{},
		ProfileKey: key, Adopted: true, Dir: fp.GameDir}
	instMu.Lock()
	err := in.save()
	instMu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := updateConfig(func(c *Config) {
		if c.LinkedInstances == nil {
			c.LinkedInstances = map[string]string{}
		}
		c.LinkedInstances[id] = fp.GameDir
	}); err != nil {
		return nil, err
	}
	j.logf("Profil „%s“ übernommen (%s %s).", fp.Name, loaderNames[loader], fp.MCVersion)
	if loader != "vanilla" {
		t, err := loadTarget("instance", id)
		if err == nil {
			if _, err := identifyTarget(j, t); err != nil {
				j.logf("Mods konnten nicht online erkannt werden: %v", err)
			}
		}
	}
	return loadInstance(id)
}

type IdentifyResult struct {
	Recognized []string `json:"recognized"`
	Unknown    []string `json:"unknown"`
}

// identifyTarget looks up jars that CraftKit does not manage yet on Modrinth and CurseForge
// (by file hash) and registers them, so updates and dependency checks work for them too.
func identifyTarget(j *Job, t *Target) (*IdentifyResult, error) {
	instMu.Lock()
	defer instMu.Unlock()
	res := &IdentifyResult{}
	managed := map[string]bool{}
	for _, it := range t.Items {
		managed[strings.ToLower(it.FileName)] = true
	}
	var cands []*idCand
	ents, _ := os.ReadDir(t.Dir)
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(n), ".jar") || managed[strings.ToLower(n)] {
			continue
		}
		h, data, err := fileSHA1(filepath.Join(t.Dir, n))
		if err != nil {
			continue
		}
		cands = append(cands, &idCand{file: n, sha1: h, finger: cfFingerprint(data)})
	}
	if len(cands) == 0 {
		return res, nil
	}
	j.setStep(fmt.Sprintf("Erkenne %d Datei(en) online …", len(cands)), -1)

	found := map[string]*InstalledItem{} // by file
	// 1) Modrinth by sha1
	var hashes []string
	for _, c := range cands {
		hashes = append(hashes, c.sha1)
	}
	var mr map[string]mrVersion
	if err := postJSON(modrinthAPI+"/version_files", nil, map[string]any{"hashes": hashes, "algorithm": "sha1"}, &mr); err != nil {
		j.logf("Modrinth-Abfrage fehlgeschlagen: %v", err)
	}
	var mrIDs []string
	for _, v := range mr {
		mrIDs = append(mrIDs, v.ProjectID)
	}
	mrProj := map[string]mrHit{}
	if len(mrIDs) > 0 {
		var list []struct {
			ID      string `json:"id"`
			Slug    string `json:"slug"`
			Title   string `json:"title"`
			IconURL string `json:"icon_url"`
		}
		if err := getJSON(modrinthAPI+"/projects?ids="+urlQueryEscape(mustJSON(uniq(mrIDs))), nil, 10*time.Minute, &list); err == nil {
			for _, p := range list {
				mrProj[p.ID] = mrHit{ProjectID: p.ID, Slug: p.Slug, Title: p.Title, IconURL: p.IconURL}
			}
		}
	}
	for _, c := range cands {
		v, ok := mr[c.sha1]
		if !ok {
			continue
		}
		mv := v.convert()
		p := mrProj[v.ProjectID]
		name := p.Title
		if name == "" {
			name = c.file
		}
		it := &InstalledItem{Key: itemKey("modrinth", v.ProjectID), Source: "modrinth", ProjectID: v.ProjectID,
			Slug: p.Slug, Name: name, IconURL: p.IconURL, PageURL: mrPage(t.Kind, p.Slug), VersionID: mv.ID,
			VersionNumber: mv.Number, VersionDate: mv.Date, FileName: c.file, InstalledAt: time.Now().Format(time.RFC3339)}
		for _, d := range mv.Deps {
			if d.ProjectID == "" {
				continue
			}
			switch d.Kind {
			case "required":
				it.Dependencies = append(it.Dependencies, itemKey("modrinth", d.ProjectID))
			case "incompatible":
				it.Incompatible = append(it.Incompatible, itemKey("modrinth", d.ProjectID))
			}
		}
		found[c.file] = it
	}

	// 2) CurseForge by fingerprint for the rest
	if cf, err := providerFor("curseforge"); err == nil {
		c := cf.(curseforge)
		var fps []uint32
		byFP := map[uint32]*idCand{}
		for _, cd := range cands {
			if found[cd.file] == nil {
				fps = append(fps, cd.finger)
				byFP[cd.finger] = cd
			}
		}
		if len(fps) > 0 {
			var r struct {
				Data struct {
					ExactMatches []struct {
						ID   int    `json:"id"`
						File cfFile `json:"file"`
					} `json:"exactMatches"`
				} `json:"data"`
			}
			if err := postJSON(fmt.Sprintf("%s/fingerprints/%d", curseforgeAPI, cfGameMinecraft), c.headers(), map[string]any{"fingerprints": fps}, &r); err != nil {
				j.logf("CurseForge-Abfrage fehlgeschlagen: %v", cfErr(err))
			}
			for _, m := range r.Data.ExactMatches {
				cd := byFP[m.File.FileFingerprint]
				if cd == nil || found[cd.file] != nil {
					continue
				}
				mv := c.convert(m.File)
				pid := strconv.Itoa(m.ID)
				it := &InstalledItem{Key: itemKey("curseforge", pid), Source: "curseforge", ProjectID: pid,
					Name: m.File.DisplayName, VersionID: mv.ID, VersionNumber: mv.Number, VersionDate: mv.Date,
					FileName: cd.file, InstalledAt: time.Now().Format(time.RFC3339)}
				if p, err := c.Project(pid); err == nil {
					it.Name, it.Slug, it.IconURL, it.PageURL = p.Name, p.Slug, p.IconURL, p.PageURL
				}
				for _, d := range mv.Deps {
					switch d.Kind {
					case "required":
						it.Dependencies = append(it.Dependencies, itemKey("curseforge", d.ProjectID))
					case "incompatible":
						it.Incompatible = append(it.Incompatible, itemKey("curseforge", d.ProjectID))
					}
				}
				found[cd.file] = it
			}
		}
	}

	// register; an item is "explicit" unless another item needs it
	needed := map[string]bool{}
	for _, it := range found {
		for _, d := range it.Dependencies {
			needed[d] = true
		}
	}
	for _, it := range t.Items {
		for _, d := range it.Dependencies {
			needed[d] = true
		}
	}
	for _, c := range cands {
		it := found[c.file]
		if it == nil {
			res.Unknown = append(res.Unknown, c.file)
			continue
		}
		if prev, ok := t.Items[it.Key]; ok && prev.FileName != it.FileName {
			// same project twice (duplicate jar) – keep the first, report the other
			res.Unknown = append(res.Unknown, c.file+" (doppelt)")
			continue
		}
		it.Explicit = !needed[it.Key]
		t.Items[it.Key] = it
		res.Recognized = append(res.Recognized, it.Name)
		j.logf("✓ erkannt: %s %s (%s)", it.Name, it.VersionNumber, sourceNames[it.Source])
	}
	for _, u := range res.Unknown {
		j.logf("? nicht erkannt: %s", u)
	}
	return res, t.save()
}

type idCand struct {
	file   string
	sha1   string
	finger uint32
}

func urlQueryEscape(s string) string { return url.QueryEscape(s) }
