package main

// Reading mod/plugin jars that are already on disk: metadata, provided ids and dependencies.

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type JarInfo struct {
	File        string   `json:"file"`
	ModID       string   `json:"modId"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Loader      string   `json:"loader"` // fabric, quilt, forge, neoforge, plugin, unknown
	Depends     []string `json:"depends"`
	Provides    []string `json:"provides"`
	Description string   `json:"description,omitempty"`
	Disabled    bool     `json:"disabled"`
	ManagedKey  string   `json:"managedKey,omitempty"`
	size        int64
	mtime       time.Time
}

// ids that are provided by the game or the loader itself
var builtinIDs = map[string]bool{
	"minecraft": true, "java": true, "fabricloader": true, "fabric-loader": true, "quilt_loader": true,
	"forge": true, "neoforge": true, "javafml": true, "lowcodefml": true, "mixinextras": false,
	"fml": true, "mcp": true, "mod_minecraftforge": true,
}

var (
	jarCacheMu sync.Mutex
	jarCache   = map[string]*JarInfo{}
)

// scanFolder reads all jars in dir (cached by size+mtime).
func scanFolder(dir string) []*JarInfo {
	ents, _ := os.ReadDir(dir)
	var out []*JarInfo
	for _, e := range ents {
		n := e.Name()
		ln := strings.ToLower(n)
		if e.IsDir() || !(strings.HasSuffix(ln, ".jar") || strings.HasSuffix(ln, ".jar.disabled")) {
			continue
		}
		p := filepath.Join(dir, n)
		st, err := e.Info()
		if err != nil {
			continue
		}
		jarCacheMu.Lock()
		c := jarCache[p]
		jarCacheMu.Unlock()
		if c == nil || c.size != st.Size() || !c.mtime.Equal(st.ModTime()) {
			c = readJar(p)
			c.size, c.mtime = st.Size(), st.ModTime()
			jarCacheMu.Lock()
			jarCache[p] = c
			jarCacheMu.Unlock()
		}
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, k int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[k].Name) })
	return out
}

func readJar(path string) *JarInfo {
	ji := &JarInfo{File: filepath.Base(path), Loader: "unknown"}
	ji.Disabled = strings.HasSuffix(strings.ToLower(path), ".disabled")
	zr, err := zip.OpenReader(path)
	if err == nil {
		defer zr.Close()
		parseJar(&zr.Reader, ji, 0)
	}
	if ji.Name == "" {
		ji.Name = strings.TrimSuffix(strings.TrimSuffix(ji.File, ".disabled"), ".jar")
	}
	ji.Depends = uniq(ji.Depends)
	ji.Provides = uniq(ji.Provides)
	return ji
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func zipFile(zr *zip.Reader, name string) []byte {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil
			}
			defer rc.Close()
			b, _ := io.ReadAll(io.LimitReader(rc, 4<<20))
			return b
		}
	}
	return nil
}

// parseJar fills ji from the jar; depth>0 means a jar nested inside another (jar-in-jar).
func parseJar(zr *zip.Reader, ji *JarInfo, depth int) {
	manifestVersion := ""
	if mf := zipFile(zr, "META-INF/MANIFEST.MF"); mf != nil {
		for _, l := range strings.Split(string(mf), "\n") {
			if strings.HasPrefix(l, "Implementation-Version:") {
				manifestVersion = strings.TrimSpace(strings.TrimPrefix(l, "Implementation-Version:"))
			}
		}
	}
	var nested []string
	switch {
	case zipFile(zr, "quilt.mod.json") != nil:
		var q struct {
			QL struct {
				ID       string `json:"id"`
				Version  string `json:"version"`
				Metadata struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"metadata"`
				Depends  []json.RawMessage `json:"depends"`
				Provides []json.RawMessage `json:"provides"`
				Jars     []string          `json:"jars"`
			} `json:"quilt_loader"`
		}
		json.Unmarshal(cleanJSON(zipFile(zr, "quilt.mod.json")), &q)
		if depth == 0 {
			ji.Loader, ji.ModID, ji.Version, ji.Name, ji.Description = "quilt", q.QL.ID, q.QL.Version, q.QL.Metadata.Name, q.QL.Metadata.Description
			for _, d := range q.QL.Depends {
				if id := rawID(d); id != "" && !isOptionalRaw(d) {
					ji.Depends = append(ji.Depends, id)
				}
			}
		}
		ji.Provides = append(ji.Provides, q.QL.ID)
		for _, p := range q.QL.Provides {
			ji.Provides = append(ji.Provides, rawID(p))
		}
		nested = q.QL.Jars
		if zipFile(zr, "fabric.mod.json") == nil {
			break
		}
		fallthrough
	case zipFile(zr, "fabric.mod.json") != nil:
		var f struct {
			ID          string         `json:"id"`
			Name        string         `json:"name"`
			Version     string         `json:"version"`
			Description string         `json:"description"`
			Depends     map[string]any `json:"depends"`
			Provides    []string       `json:"provides"`
			Jars        []struct {
				File string `json:"file"`
			} `json:"jars"`
		}
		json.Unmarshal(cleanJSON(zipFile(zr, "fabric.mod.json")), &f)
		if depth == 0 && ji.ModID == "" {
			ji.Loader, ji.ModID, ji.Version, ji.Name, ji.Description = "fabric", f.ID, f.Version, f.Name, f.Description
			for id := range f.Depends {
				ji.Depends = append(ji.Depends, id)
			}
		}
		ji.Provides = append(ji.Provides, f.ID)
		ji.Provides = append(ji.Provides, f.Provides...)
		for _, j := range f.Jars {
			nested = append(nested, j.File)
		}
	case zipFile(zr, "META-INF/neoforge.mods.toml") != nil || zipFile(zr, "META-INF/mods.toml") != nil:
		loader := "forge"
		b := zipFile(zr, "META-INF/mods.toml")
		if nb := zipFile(zr, "META-INF/neoforge.mods.toml"); nb != nil {
			b, loader = nb, "neoforge"
		}
		mods, deps := parseModsToml(string(b))
		for i, m := range mods {
			ver := m["version"]
			if strings.Contains(ver, "${") {
				ver = manifestVersion
			}
			if i == 0 && depth == 0 {
				ji.Loader, ji.ModID, ji.Name, ji.Version, ji.Description = loader, m["modId"], m["displayName"], ver, m["description"]
			}
			ji.Provides = append(ji.Provides, m["modId"])
		}
		if depth == 0 {
			ji.Depends = append(ji.Depends, deps...)
		}
		for _, f := range zr.File {
			if strings.HasPrefix(f.Name, "META-INF/jarjar/") && strings.HasSuffix(f.Name, ".jar") {
				nested = append(nested, f.Name)
			}
		}
	case zipFile(zr, "mcmod.info") != nil:
		var list []struct {
			ModID        string   `json:"modid"`
			Name         string   `json:"name"`
			Version      string   `json:"version"`
			Description  string   `json:"description"`
			Dependencies []string `json:"dependencies"`
		}
		raw := cleanJSON(zipFile(zr, "mcmod.info"))
		if err := json.Unmarshal(raw, &list); err != nil {
			var wrapped struct {
				ModList json.RawMessage `json:"modList"`
			}
			json.Unmarshal(raw, &wrapped)
			json.Unmarshal(wrapped.ModList, &list)
		}
		for i, m := range list {
			if i == 0 && depth == 0 {
				ji.Loader, ji.ModID, ji.Name, ji.Version, ji.Description = "forge", m.ModID, m.Name, m.Version, m.Description
				ji.Depends = append(ji.Depends, m.Dependencies...)
			}
			ji.Provides = append(ji.Provides, m.ModID)
		}
	case zipFile(zr, "paper-plugin.yml") != nil || zipFile(zr, "plugin.yml") != nil || zipFile(zr, "bungee.yml") != nil:
		b := zipFile(zr, "plugin.yml")
		paper := false
		if b == nil {
			b = zipFile(zr, "bungee.yml")
		}
		if pb := zipFile(zr, "paper-plugin.yml"); pb != nil {
			b, paper = pb, true
		}
		name, ver, desc, deps := parsePluginYml(string(b), paper)
		if depth == 0 {
			ji.Loader, ji.ModID, ji.Name, ji.Version, ji.Description, ji.Depends = "plugin", name, name, ver, desc, deps
		}
		ji.Provides = append(ji.Provides, name)
	}
	// jar-in-jar: bundled libraries count as provided
	if depth < 2 {
		for _, n := range nested {
			b := zipFile(zr, n)
			if b == nil {
				continue
			}
			inner, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
			if err != nil {
				continue
			}
			sub := &JarInfo{}
			parseJar(inner, sub, depth+1)
			ji.Provides = append(ji.Provides, sub.Provides...)
		}
	}
}

var jsonComment = regexp.MustCompile(`(?m)^\s*//.*$`)

// cleanJSON removes BOM and line comments that some mods ship in their metadata.
func cleanJSON(b []byte) []byte {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	b = jsonComment.ReplaceAll(b, nil)
	// raw newlines inside strings break encoding/json – replace control chars with spaces
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == '\n' || c == '\r' || c == '\t' {
			c = ' '
		}
		out = append(out, c)
	}
	return out
}

func rawID(r json.RawMessage) string {
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	var o struct {
		ID string `json:"id"`
	}
	json.Unmarshal(r, &o)
	return o.ID
}

func isOptionalRaw(r json.RawMessage) bool {
	var o struct {
		Optional bool `json:"optional"`
	}
	json.Unmarshal(r, &o)
	return o.Optional
}

// parseModsToml is a tiny parser for the parts of (neo)forge mods.toml we need.
func parseModsToml(s string) (mods []map[string]string, requiredDeps []string) {
	var cur map[string]string
	section := ""
	type dep struct{ id, mandatory, typ, side string }
	var d *dep
	flush := func() {
		if d != nil && d.id != "" {
			required := d.mandatory == "true" || d.typ == "required"
			if required && d.side != "SERVER" {
				requiredDeps = append(requiredDeps, d.id)
			}
		}
		d = nil
	}
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	inMultiline := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if inMultiline {
			if strings.Contains(line, `'''`) || strings.Contains(line, `"""`) {
				inMultiline = false
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[[") {
			flush()
			section = strings.Trim(line, "[] ")
			if section == "mods" {
				cur = map[string]string{}
				mods = append(mods, cur)
			} else if strings.HasPrefix(section, "dependencies") {
				d = &dep{}
				cur = nil
			} else {
				cur = nil
			}
			continue
		}
		if strings.HasPrefix(line, "[") {
			flush()
			section = strings.Trim(line, "[] ")
			cur = nil
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		if strings.HasPrefix(v, `'''`) || strings.HasPrefix(v, `"""`) {
			rest := v[3:]
			if !strings.Contains(rest, `'''`) && !strings.Contains(rest, `"""`) {
				inMultiline = true
			}
			v = ""
		}
		if i := strings.Index(v, " #"); i > 0 && !strings.HasPrefix(v, `"`) {
			v = strings.TrimSpace(v[:i])
		}
		v = strings.Trim(v, `"'`)
		if cur != nil {
			cur[k] = v
		} else if d != nil {
			switch k {
			case "modId":
				d.id = v
			case "mandatory":
				d.mandatory = v
			case "type":
				d.typ = strings.ToLower(v)
			case "side":
				d.side = strings.ToUpper(v)
			}
		}
	}
	flush()
	return
}

// parsePluginYml extracts name, version and hard dependencies from plugin.yml / paper-plugin.yml.
func parsePluginYml(s string, paper bool) (name, version, desc string, deps []string) {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	val := func(l string) string {
		i := strings.Index(l, ":")
		return strings.Trim(strings.TrimSpace(l[i+1:]), `"'`)
	}
	listKey := ""
	paperDep := ""
	for _, raw := range lines {
		if strings.HasPrefix(strings.TrimSpace(raw), "#") || strings.TrimSpace(raw) == "" {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		l := strings.TrimSpace(raw)
		if indent == 0 {
			listKey = ""
			switch {
			case strings.HasPrefix(l, "name:"):
				name = val(l)
			case strings.HasPrefix(l, "version:"):
				version = val(l)
			case strings.HasPrefix(l, "description:"):
				desc = val(l)
			case strings.HasPrefix(l, "depend:"):
				v := val(l)
				if strings.HasPrefix(v, "[") {
					for _, x := range strings.Split(strings.Trim(v, "[]"), ",") {
						deps = append(deps, strings.Trim(strings.TrimSpace(x), `"'`))
					}
				} else if v == "" {
					listKey = "depend"
				} else {
					deps = append(deps, v)
				}
			case strings.HasPrefix(l, "dependencies:"):
				listKey = "dependencies"
			}
			continue
		}
		if listKey == "depend" && strings.HasPrefix(l, "-") {
			deps = append(deps, strings.Trim(strings.TrimSpace(l[1:]), `"'`))
		}
		if listKey == "dependencies" && paper {
			// dependencies: server: <Name>: required: true
			if strings.HasSuffix(l, ":") && indent >= 4 {
				paperDep = strings.TrimSuffix(l, ":")
			} else if strings.HasPrefix(l, "required:") && val(l) == "true" && paperDep != "" {
				deps = append(deps, paperDep)
			}
		}
	}
	return
}

// MissingDep describes a dependency that no jar in the folder provides.
type MissingDep struct {
	ModID    string   `json:"modId"`
	NeededBy []string `json:"neededBy"`
	// suggestion from Modrinth, if a project with a matching slug exists
	Source    string `json:"source,omitempty"`
	ProjectID string `json:"projectId,omitempty"`
	Name      string `json:"name,omitempty"`
	IconURL   string `json:"iconUrl,omitempty"`
}

var depAliases = map[string]string{
	"fabric": "fabric-api", "fabric-api-base": "fabric-api", "cloth-config2": "cloth-config", "cloth_config": "cloth-config",
	"architectury": "architectury-api", "forgeconfigapiport": "forge-config-api-port", "yet_another_config_lib_v3": "yacl",
	"yet-another-config-lib": "yacl", "owo": "owo-lib", "kotlinforforge": "kotlin-for-forge", "fabric_language_kotlin": "fabric-language-kotlin",
	"roughlyenoughitems": "rei", "puzzleslib": "puzzles-lib", "resourcefullib": "resourceful-lib", "geckolib3": "geckolib",
	"playeranimator": "playeranimator", "creativecore": "creativecore", "collective": "collective", "bookshelf": "bookshelf-lib",
	"moonlight": "moonlight", "supermartijn642corelib": "supermartijn642s-core-lib", "cristellib": "cristel-lib",
	"placeholderapi": "placeholderapi", "vault": "vault", "protocollib": "protocollib", "luckperms": "luckperms",
	"worldedit": "worldedit", "fawe": "fastasyncworldedit",
}

// missingDeps checks which required ids are not provided by any enabled jar.
func missingDeps(jars []*JarInfo, kind string) []*MissingDep {
	provided := map[string]bool{}
	for _, j := range jars {
		if j.Disabled {
			continue
		}
		for _, p := range j.Provides {
			provided[strings.ToLower(p)] = true
		}
	}
	byID := map[string]*MissingDep{}
	var order []string
	for _, j := range jars {
		if j.Disabled {
			continue
		}
		for _, d := range j.Depends {
			ld := strings.ToLower(d)
			if builtinIDs[ld] || provided[ld] {
				continue
			}
			if kind == "plugin" && provided[ld] {
				continue
			}
			m := byID[ld]
			if m == nil {
				m = &MissingDep{ModID: d}
				byID[ld] = m
				order = append(order, ld)
			}
			if !contains(m.NeededBy, j.Name) {
				m.NeededBy = append(m.NeededBy, j.Name)
			}
		}
	}
	var out []*MissingDep
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}

// suggestForMissing looks up Modrinth projects whose slug matches the missing id.
func suggestForMissing(missing []*MissingDep) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, m := range missing {
		wg.Add(1)
		go func(m *MissingDep) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			id := strings.ToLower(m.ModID)
			cands := []string{}
			if a, ok := depAliases[id]; ok {
				cands = append(cands, a)
			}
			cands = append(cands, id, strings.ReplaceAll(id, "_", "-"))
			for _, c := range uniq(cands) {
				p, err := (modrinth{}).Project(c)
				if err == nil && p != nil {
					m.Source, m.ProjectID, m.Name, m.IconURL = "modrinth", p.ID, p.Name, p.IconURL
					return
				}
			}
		}(m)
	}
	wg.Wait()
}

// ---------- hashes for online identification ----------

func fileSHA1(path string) (string, []byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:]), b, nil
}

// cfFingerprint is CurseForge's MurmurHash2 (seed 1) over the file without whitespace bytes.
func cfFingerprint(data []byte) uint32 {
	buf := make([]byte, 0, len(data))
	for _, c := range data {
		if c != 9 && c != 10 && c != 13 && c != 32 {
			buf = append(buf, c)
		}
	}
	const m = 0x5bd1e995
	const r = 24
	n := len(buf)
	h := uint32(1) ^ uint32(n)
	i := 0
	for ; n-i >= 4; i += 4 {
		k := uint32(buf[i]) | uint32(buf[i+1])<<8 | uint32(buf[i+2])<<16 | uint32(buf[i+3])<<24
		k *= m
		k ^= k >> r
		k *= m
		h *= m
		h ^= k
	}
	switch n - i {
	case 3:
		h ^= uint32(buf[i+2]) << 16
		fallthrough
	case 2:
		h ^= uint32(buf[i+1]) << 8
		fallthrough
	case 1:
		h ^= uint32(buf[i])
		h *= m
	}
	h ^= h >> 13
	h *= m
	h ^= h >> 15
	return h
}
