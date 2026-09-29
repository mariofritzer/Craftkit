package main

// Exporting an instance as a Modrinth modpack (.mrpack) to share it with friends.

import (
	"archive/zip"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ExportOptions struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Config        bool   `json:"config"`
	ResourcePacks bool   `json:"resourcePacks"`
	ShaderPacks   bool   `json:"shaderPacks"`
	Options       bool   `json:"options"`
	Servers       bool   `json:"servers"`
}

type ExportResult struct {
	Path     string   `json:"-"`
	FileName string   `json:"fileName"`
	Linked   int      `json:"linked"` // downloaded from Modrinth by the importer
	Packed   []string `json:"packed"` // jars packed into the file
	Size     int64    `json:"size"`
}

type mrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

func fileHashes(p string) (sha1hex, sha512hex string, size int64, err error) {
	f, err := os.Open(p)
	if err != nil {
		return "", "", 0, err
	}
	defer f.Close()
	h1, h5 := sha1.New(), sha512.New()
	size, err = io.Copy(io.MultiWriter(h1, h5), f)
	return hex.EncodeToString(h1.Sum(nil)), hex.EncodeToString(h5.Sum(nil)), size, err
}

func exportInstance(id string, o ExportOptions) (*ExportResult, error) {
	in, err := loadInstance(id)
	if err != nil {
		return nil, err
	}
	if in.Loader == "vanilla" && len(in.Items) == 0 && !o.Config && !o.ResourcePacks {
		return nil, errNew("die Instanz enthält nichts zum Teilen")
	}
	if strings.TrimSpace(o.Name) == "" {
		o.Name = in.Name
	}
	if strings.TrimSpace(o.Version) == "" {
		o.Version = time.Now().Format("2006.01.02")
	}
	deps := map[string]string{"minecraft": in.MCVersion}
	switch in.Loader {
	case "fabric":
		deps["fabric-loader"] = in.LoaderVersion
	case "quilt":
		deps["quilt-loader"] = in.LoaderVersion
	case "forge":
		deps["forge"] = forgeShort(in.LoaderVersion)
	case "neoforge":
		deps["neoforge"] = in.LoaderVersion
	}

	modsDir := filepath.Join(in.Dir, "mods")
	res := &ExportResult{}
	// look up Modrinth download URLs for the managed mods in one request
	type managed struct {
		it     *InstalledItem
		folder string
	}
	var all []managed
	for _, it := range in.Items {
		all = append(all, managed{it, "mods"})
	}
	if o.ResourcePacks {
		for _, it := range in.ResourcePacks {
			all = append(all, managed{it, "resourcepacks"})
		}
	}
	if o.ShaderPacks {
		for _, it := range in.Shaders {
			all = append(all, managed{it, "shaderpacks"})
		}
	}
	var mrIDs []string
	for _, m := range all {
		if m.it.Source == "modrinth" && !m.it.Disabled {
			mrIDs = append(mrIDs, m.it.VersionID)
		}
	}
	mrVersions := map[string]mrVersion{}
	sides := map[string][2]string{}
	if len(mrIDs) > 0 {
		var list []mrVersion
		if err := getJSON(modrinthAPI+"/versions?ids="+urlQueryEscape(mustJSON(mrIDs)), nil, 10*time.Minute, &list); err == nil {
			var pids []string
			for _, v := range list {
				mrVersions[v.ID] = v
				pids = append(pids, v.ProjectID)
			}
			var projects []struct {
				ID         string `json:"id"`
				ClientSide string `json:"client_side"`
				ServerSide string `json:"server_side"`
			}
			if getJSON(modrinthAPI+"/projects?ids="+urlQueryEscape(mustJSON(uniq(pids))), nil, 10*time.Minute, &projects) == nil {
				for _, p := range projects {
					sides[p.ID] = [2]string{p.ClientSide, p.ServerSide}
				}
			}
		}
	}

	envFor := func(side string) string {
		switch side {
		case "unsupported":
			return "unsupported"
		case "optional":
			return "optional"
		}
		return "required"
	}
	var files []mrpackFile
	var packMods []string       // jar names copied into overrides/mods
	linked := map[string]bool{} // "folder/file" in lower case
	for _, m := range all {
		it := m.it
		if it.Disabled || it.Source != "modrinth" {
			continue
		}
		p := filepath.Join(in.Dir, m.folder, it.FileName)
		v, ok := mrVersions[it.VersionID]
		if !ok || !fileExists(p) {
			continue
		}
		s1, s5, size, err := fileHashes(p)
		if err != nil {
			continue
		}
		for _, f := range v.Files {
			// only link when the file on disk is exactly the one Modrinth serves
			if strings.EqualFold(f.Hashes["sha1"], s1) && strings.HasPrefix(f.URL, "https://cdn.modrinth.com/") {
				mf := mrpackFile{Path: m.folder + "/" + it.FileName, Hashes: map[string]string{"sha1": s1, "sha512": s5},
					Downloads: []string{f.URL}, FileSize: size}
				if sd, ok := sides[v.ProjectID]; ok && m.folder == "mods" {
					mf.Env = map[string]string{"client": envFor(sd[0]), "server": envFor(sd[1])}
				} else if m.folder != "mods" {
					mf.Env = map[string]string{"client": "required", "server": "unsupported"}
				}
				files = append(files, mf)
				linked[strings.ToLower(m.folder+"/"+it.FileName)] = true
				break
			}
		}
	}
	// everything else in mods/ (CurseForge, manually added) is packed
	ents, _ := os.ReadDir(modsDir)
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(n), ".jar") || linked[strings.ToLower("mods/"+n)] {
			continue
		}
		packMods = append(packMods, n)
	}
	res.Linked = len(files)
	res.Packed = packMods

	idx := map[string]any{
		"formatVersion": 1, "game": "minecraft", "versionId": o.Version, "name": o.Name,
		"summary": sprintf("Mit CraftKit exportiert aus „%s“", in.Name), "files": files, "dependencies": deps,
	}
	if files == nil {
		idx["files"] = []mrpackFile{}
	}

	tmpDir := filepath.Join(dataDir(), "tmp")
	os.MkdirAll(tmpDir, 0o755)
	res.FileName = safeFileName(fmt.Sprintf("%s-%s.mrpack", o.Name, o.Version))
	res.Path = filepath.Join(tmpDir, fmt.Sprintf("export-%d.mrpack", time.Now().UnixNano()))
	out, err := os.Create(res.Path)
	if err != nil {
		return nil, err
	}
	zw := zip.NewWriter(out)
	fail := func(err error) (*ExportResult, error) {
		zw.Close()
		out.Close()
		os.Remove(res.Path)
		return nil, err
	}
	w, err := zw.Create("modrinth.index.json")
	if err != nil {
		return fail(err)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(idx); err != nil {
		return fail(err)
	}
	addFile := func(src, name string) error {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer f.Close()
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, f)
		return err
	}
	addDir := func(dir, prefix string, skip func(rel string) bool) error {
		if !fileExists(dir) {
			return nil
		}
		return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			rel = filepath.ToSlash(rel)
			if skip != nil && skip(rel) {
				return nil
			}
			return addFile(p, prefix+rel)
		})
	}
	for _, n := range packMods {
		if err := addFile(filepath.Join(modsDir, n), "overrides/mods/"+n); err != nil {
			return fail(err)
		}
	}
	if o.Config {
		for _, d := range []string{"config", "defaultconfigs", "kubejs"} {
			if err := addDir(filepath.Join(in.Dir, d), "overrides/"+d+"/", nil); err != nil {
				return fail(err)
			}
		}
	}
	if o.ResourcePacks {
		if err := addDir(filepath.Join(in.Dir, "resourcepacks"), "overrides/resourcepacks/", func(rel string) bool {
			return linked[strings.ToLower("resourcepacks/"+rel)] || strings.HasSuffix(rel, ".disabled")
		}); err != nil {
			return fail(err)
		}
	}
	if o.ShaderPacks {
		if err := addDir(filepath.Join(in.Dir, "shaderpacks"), "overrides/shaderpacks/", func(rel string) bool {
			return strings.HasSuffix(rel, ".txt") || linked[strings.ToLower("shaderpacks/"+rel)] || strings.HasSuffix(rel, ".disabled")
		}); err != nil {
			return fail(err)
		}
	}
	if o.Options && fileExists(filepath.Join(in.Dir, "options.txt")) {
		if err := addFile(filepath.Join(in.Dir, "options.txt"), "client-overrides/options.txt"); err != nil {
			return fail(err)
		}
	}
	if o.Servers && fileExists(filepath.Join(in.Dir, "servers.dat")) {
		if err := addFile(filepath.Join(in.Dir, "servers.dat"), "client-overrides/servers.dat"); err != nil {
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(res.Path)
		return nil, err
	}
	if st, err := os.Stat(res.Path); err == nil {
		res.Size = st.Size()
	}
	return res, nil
}
