package main

// Reading modpack files into a common model so the user can pick what to import
// (single mods, resource packs, shaders, settings …) and where to put it
// (a new instance or an existing one with the same Minecraft version and loader).

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// packFile is one selectable thing in a modpack: a mod, resource pack or shader pack.
type packFile struct {
	ID         string
	Rel        string // destination relative to the game folder (mods/x.jar)
	Name       string
	Kind       string // mod, resourcepack, shader
	URLs       []string
	Hash       *Hash
	Size       int64
	ProjectKey string   // modrinth:<id> / curseforge:<id>, if known
	Requires   []string // project keys of required dependencies
	ManualURL  string   // CurseForge files that must be downloaded by hand
	IconURL    string
	sha1       string
	zfs        []*zip.File // files taken from the overrides folder
	zprefix    string      // their prefix inside the archive
}

type overrideFile struct {
	zf  *zip.File
	rel string
}

type packModel struct {
	Format, Name, Version, Summary, MC, Loader, LV string
	Files                                         []*packFile
	Groups                                        map[string][]overrideFile // config, options, servers, saves
	Skipped                                       int
	zr                                            *zip.ReadCloser
}

func (m *packModel) Close() { m.zr.Close() }

// readPack opens a modpack file and describes its content (needs the internet for CurseForge packs).
func readPack(file string) (*packModel, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, errf("Datei ist kein gültiges Modpack (kein Zip): %w", err)
	}
	m := &packModel{zr: zr, Groups: map[string][]overrideFile{}}
	if b := zipFile(&zr.Reader, "modrinth.index.json"); b != nil {
		err = m.readMrpack(b)
	} else if b := zipFile(&zr.Reader, "manifest.json"); b != nil {
		err = m.readCurseForge(b)
	} else {
		err = errNew("unbekanntes Modpack-Format – unterstützt werden Modrinth (.mrpack) und CurseForge (.zip mit manifest.json)")
	}
	if err != nil {
		zr.Close()
		return nil, err
	}
	return m, nil
}

func kindForFolder(folder string) string {
	switch folder {
	case "mods":
		return "mod"
	case "resourcepacks":
		return "resourcepack"
	case "shaderpacks":
		return "shader"
	}
	return ""
}

func displayName(file string) string {
	n := strings.TrimSuffix(strings.TrimSuffix(file, ".disabled"), path.Ext(strings.TrimSuffix(file, ".disabled")))
	return strings.TrimSpace(n)
}

func (m *packModel) readMrpack(raw []byte) error {
	var idx mrpackIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return errf("modrinth.index.json nicht lesbar: %w", err)
	}
	if idx.Game != "" && idx.Game != "minecraft" {
		return errf("Modpack ist nicht für Minecraft")
	}
	m.Format, m.Name, m.Version, m.Summary = "modrinth", idx.Name, idx.VersionID, idx.Summary
	m.MC = idx.Dependencies["minecraft"]
	if m.MC == "" {
		return errNew("Modpack nennt keine Minecraft-Version")
	}
	m.Loader = "vanilla"
	switch {
	case idx.Dependencies["neoforge"] != "":
		m.Loader, m.LV = "neoforge", idx.Dependencies["neoforge"]
	case idx.Dependencies["forge"] != "":
		m.Loader, m.LV = "forge", forgeFullVersion(m.MC, idx.Dependencies["forge"])
	case idx.Dependencies["quilt-loader"] != "":
		m.Loader, m.LV = "quilt", idx.Dependencies["quilt-loader"]
	case idx.Dependencies["fabric-loader"] != "":
		m.Loader, m.LV = "fabric", idx.Dependencies["fabric-loader"]
	}
	for _, f := range idx.Files {
		if f.Env["client"] == "unsupported" {
			m.Skipped++
			continue
		}
		if _, err := safeJoin("x", f.Path); err != nil {
			return err
		}
		rel := path.Clean(strings.ReplaceAll(f.Path, "\\", "/"))
		pf := &packFile{ID: "f:" + rel, Rel: rel, Name: displayName(path.Base(rel)), Size: f.FileSize, sha1: f.Hashes["sha1"]}
		pf.Kind = kindForFolder(strings.SplitN(rel, "/", 2)[0])
		if pf.Kind == "" {
			pf.Kind = "other"
		}
		if v := f.Hashes["sha512"]; v != "" {
			pf.Hash = &Hash{"sha512", v}
		} else if v := f.Hashes["sha1"]; v != "" {
			pf.Hash = &Hash{"sha1", v}
		}
		for _, u := range f.Downloads {
			if strings.HasPrefix(u, "https://") {
				pf.URLs = append(pf.URLs, u)
			}
		}
		m.Files = append(m.Files, pf)
	}
	m.readOverrides("overrides/")
	m.readOverrides("client-overrides/")
	return nil
}

// readOverrides sorts the files of an overrides folder into selectable entries and groups.
func (m *packModel) readOverrides(prefix string) {
	units := map[string]*packFile{}
	for _, pf := range m.Files {
		if strings.HasPrefix(pf.ID, "o:") {
			units[pf.Rel] = pf
		}
	}
	for _, f := range m.zr.File {
		if !strings.HasPrefix(f.Name, prefix) || f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		if rel == "" {
			continue
		}
		parts := strings.SplitN(rel, "/", 3)
		if kind := kindForFolder(parts[0]); kind != "" && len(parts) >= 2 {
			unit := parts[0] + "/" + parts[1]
			pf := units[unit]
			if pf == nil {
				pf = &packFile{ID: "o:" + unit, Rel: unit, Name: displayName(parts[1]), Kind: kind}
				units[unit] = pf
				m.Files = append(m.Files, pf)
			}
			pf.zfs = append(pf.zfs, f)
			pf.zprefix = prefix
			pf.Size += int64(f.UncompressedSize64)
			continue
		}
		group := "config"
		switch {
		case rel == "servers.dat":
			group = "servers"
		case len(parts) == 1 && strings.HasPrefix(rel, "options") && strings.HasSuffix(rel, ".txt"):
			group = "options"
		case parts[0] == "saves":
			group = "saves"
		}
		m.Groups[group] = append(m.Groups[group], overrideFile{f, rel})
	}
}

func (m *packModel) readCurseForge(raw []byte) error {
	var man cfManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return errf("manifest.json nicht lesbar: %w", err)
	}
	m.Format, m.Name, m.Version, m.MC = "curseforge", man.Name, man.Version, man.Minecraft.Version
	if m.MC == "" {
		return errNew("Modpack nennt keine Minecraft-Version")
	}
	m.Loader = "vanilla"
	for _, l := range man.Minecraft.ModLoaders {
		if !l.Primary && len(man.Minecraft.ModLoaders) > 1 {
			continue
		}
		id := l.ID
		switch {
		case strings.HasPrefix(id, "neoforge-"):
			m.Loader, m.LV = "neoforge", strings.TrimPrefix(id, "neoforge-")
			if strings.HasPrefix(m.LV, m.MC+"-") { // 1.20.1 NeoForge uses forge-style versions
				m.Loader, m.LV = "forge", forgeFullVersion(m.MC, strings.TrimPrefix(m.LV, m.MC+"-"))
			}
		case strings.HasPrefix(id, "forge-"):
			m.Loader, m.LV = "forge", forgeFullVersion(m.MC, strings.TrimPrefix(id, "forge-"))
		case strings.HasPrefix(id, "fabric-"):
			m.Loader, m.LV = "fabric", strings.TrimPrefix(id, "fabric-")
		case strings.HasPrefix(id, "quilt-"):
			m.Loader, m.LV = "quilt", strings.TrimPrefix(id, "quilt-")
		}
	}
	if len(man.Files) > 0 {
		p, err := providerFor("curseforge")
		if err != nil {
			return errf("CurseForge-Modpacks brauchen einen CurseForge-API-Key (Einstellungen)")
		}
		cf := p.(curseforge)
		var fileIDs, modIDs []int
		for _, f := range man.Files {
			fileIDs = append(fileIDs, f.FileID)
			modIDs = append(modIDs, f.ProjectID)
		}
		files := map[int]cfFile{}
		for i := 0; i < len(fileIDs); i += 500 {
			var r struct {
				Data []cfFile `json:"data"`
			}
			if err := postJSON(curseforgeAPI+"/mods/files", cf.headers(), map[string]any{"fileIds": fileIDs[i:min(i+500, len(fileIDs))]}, &r); err != nil {
				return cfErr(err)
			}
			for _, f := range r.Data {
				files[f.ID] = f
			}
		}
		mods := map[int]cfMod{}
		for i := 0; i < len(modIDs); i += 500 {
			var r struct {
				Data []cfMod `json:"data"`
			}
			if err := postJSON(curseforgeAPI+"/mods", cf.headers(), map[string]any{"modIds": modIDs[i:min(i+500, len(modIDs))]}, &r); err != nil {
				return cfErr(err)
			}
			for _, md := range r.Data {
				mods[md.ID] = md
			}
		}
		for _, f := range man.Files {
			file, ok := files[f.FileID]
			md := mods[f.ProjectID]
			pf := &packFile{ID: "cf:" + strconv.Itoa(f.FileID), Name: md.Name, ProjectKey: "curseforge:" + strconv.Itoa(f.ProjectID), }
			if md.Logo != nil {
				pf.IconURL = md.Logo.ThumbnailURL
			}
			if pf.Name == "" {
				pf.Name = "Projekt " + strconv.Itoa(f.ProjectID)
			}
			folder := "mods"
			switch md.ClassID {
			case 12:
				folder = "resourcepacks"
			case 6552:
				folder = "shaderpacks"
			case 6945:
				folder = "datapacks-craftkit" // data packs belong into a world; keep them aside
			}
			pf.Kind = kindForFolder(folder)
			if pf.Kind == "" {
				pf.Kind = "other"
			}
			if !ok {
				pf.ManualURL = "-"
				m.Files = append(m.Files, pf)
				continue
			}
			pf.Rel = folder + "/" + safeFileName(file.FileName)
			pf.Size = file.FileLength
			for _, d := range file.Dependencies {
				if d.RelationType == 3 {
					pf.Requires = append(pf.Requires, "curseforge:"+strconv.Itoa(d.ModID))
				}
			}
			if file.DownloadURL == "" {
				page := md.Links.WebsiteURL
				if page == "" {
					page = "https://www.curseforge.com/projects/" + strconv.Itoa(f.ProjectID)
				}
				pf.ManualURL = page + "/files/" + strconv.Itoa(file.ID)
			} else {
				pf.URLs = []string{file.DownloadURL}
			}
			for _, x := range file.Hashes {
				if x.Algo == 1 {
					pf.Hash = &Hash{"sha1", x.Value}
				}
			}
			m.Files = append(m.Files, pf)
		}
	}
	over := strings.TrimSuffix(man.Overrides, "/")
	if over == "" {
		over = "overrides"
	}
	m.readOverrides(over + "/")
	return nil
}

// lookupModrinth fills in names, icons, project ids and dependencies from Modrinth (best effort).
func (m *packModel) lookupModrinth() {
	bySha := map[string][]*packFile{}
	for _, pf := range m.Files {
		if pf.ProjectKey != "" {
			continue
		}
		if pf.sha1 == "" && len(pf.zfs) == 1 && pf.Kind == "mod" {
			if rc, err := pf.zfs[0].Open(); err == nil {
				h := sha1.New()
				io.Copy(h, io.LimitReader(rc, 512<<20))
				rc.Close()
				pf.sha1 = hex.EncodeToString(h.Sum(nil))
			}
		}
		if pf.sha1 != "" {
			bySha[pf.sha1] = append(bySha[pf.sha1], pf)
		}
	}
	if len(bySha) == 0 {
		return
	}
	var hashes []string
	for h := range bySha {
		hashes = append(hashes, h)
	}
	var mr map[string]mrVersion
	if err := postJSON(modrinthAPI+"/version_files", nil, map[string]any{"hashes": hashes, "algorithm": "sha1"}, &mr); err != nil {
		logf("modpack lookup: %v", err)
		return
	}
	var ids []string
	for h, v := range mr {
		for _, pf := range bySha[h] {
			pf.ProjectKey = "modrinth:" + v.ProjectID
			for _, d := range v.Dependencies {
				if d.DependencyType == "required" && d.ProjectID != "" {
					pf.Requires = append(pf.Requires, "modrinth:"+d.ProjectID)
				}
			}
		}
		ids = append(ids, v.ProjectID)
	}
	var list []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		IconURL string `json:"icon_url"`
	}
	if len(ids) > 0 && getJSON(modrinthAPI+"/projects?ids="+urlQueryEscape(mustJSON(uniq(ids))), nil, 10*time.Minute, &list) == nil {
		for _, p := range list {
			for _, pf := range m.Files {
				if pf.ProjectKey == "modrinth:"+p.ID {
					pf.Name, pf.IconURL = p.Title, p.IconURL
				}
			}
		}
	}
}

// ---------- what the UI sees ----------

type PackEntry struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	File     string   `json:"file"`
	Kind     string   `json:"kind"`
	Size     int64    `json:"size"`
	IconURL  string   `json:"iconUrl,omitempty"`
	Key      string   `json:"key,omitempty"`
	Requires []string `json:"requires,omitempty"` // ids of entries in this pack
	Manual   bool     `json:"manual,omitempty"`
	Missing  bool     `json:"missing,omitempty"`
}

type PackGroup struct {
	ID    string `json:"id"`
	Files int    `json:"files"`
	Size  int64  `json:"size"`
}

type PackTarget struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	LoaderVersion string `json:"loaderVersion"`
}

type PackInfo struct {
	Key           string       `json:"key"`
	FileName      string       `json:"fileName"`
	Format        string       `json:"format"`
	Name          string       `json:"name"`
	Version       string       `json:"version"`
	Summary       string       `json:"summary"`
	MCVersion     string       `json:"mcVersion"`
	Loader        string       `json:"loader"`
	LoaderVersion string       `json:"loaderVersion"`
	Entries       []PackEntry  `json:"entries"`
	Groups        []PackGroup  `json:"groups"`
	Skipped       int          `json:"skipped"`
	Instances     []PackTarget `json:"instances"` // existing instances the content fits into
}

var (
	pendingMu    sync.Mutex
	pendingPacks = map[string]pendingPack{}
)

type pendingPack struct {
	file, fileName string
	ref            ModpackRef
	created        time.Time
}

// rememberPack keeps an uploaded/downloaded pack file until the user decides what to import.
func rememberPack(file, fileName string, ref ModpackRef) string {
	b := make([]byte, 12)
	rand.Read(b)
	key := hex.EncodeToString(b)
	pendingMu.Lock()
	defer pendingMu.Unlock()
	for k, p := range pendingPacks {
		if time.Since(p.created) > 3*time.Hour {
			os.Remove(p.file)
			delete(pendingPacks, k)
		}
	}
	pendingPacks[key] = pendingPack{file, fileName, ref, time.Now()}
	return key
}

func takePending(key string, remove bool) (pendingPack, bool) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	p, ok := pendingPacks[key]
	if ok && remove {
		delete(pendingPacks, key)
	}
	return p, ok
}

func inspectPack(key string) (*PackInfo, error) {
	pp, ok := takePending(key, false)
	if !ok {
		return nil, errNew("Die Modpack-Datei ist nicht mehr da – bitte noch einmal auswählen.")
	}
	m, err := readPack(pp.file)
	if err != nil {
		return nil, err
	}
	defer m.Close()
	m.lookupModrinth()
	info := &PackInfo{Key: key, FileName: pp.fileName, Format: m.Format, Name: firstNonEmpty(pp.ref.Name, m.Name), Version: firstNonEmpty(pp.ref.Version, m.Version),
		Summary: m.Summary, MCVersion: m.MC, Loader: m.Loader, LoaderVersion: m.LV, Skipped: m.Skipped}
	byKey := map[string]string{}
	for _, pf := range m.Files {
		if pf.ProjectKey != "" {
			byKey[pf.ProjectKey] = pf.ID
		}
	}
	for _, pf := range m.Files {
		e := PackEntry{ID: pf.ID, Name: pf.Name, File: path.Base(pf.Rel), Kind: pf.Kind, Size: pf.Size, IconURL: pf.IconURL, Key: pf.ProjectKey,
			Manual: pf.ManualURL != "" && pf.ManualURL != "-", Missing: pf.ManualURL == "-"}
		for _, r := range pf.Requires {
			if id, ok := byKey[r]; ok && id != pf.ID {
				e.Requires = append(e.Requires, id)
			}
		}
		info.Entries = append(info.Entries, e)
	}
	sort.SliceStable(info.Entries, func(a, b int) bool {
		ka, kb := kindOrder(info.Entries[a].Kind), kindOrder(info.Entries[b].Kind)
		if ka != kb {
			return ka < kb
		}
		return strings.ToLower(info.Entries[a].Name) < strings.ToLower(info.Entries[b].Name)
	})
	for _, g := range []string{"config", "options", "servers", "saves"} {
		if fs := m.Groups[g]; len(fs) > 0 {
			pg := PackGroup{ID: g, Files: len(fs)}
			for _, f := range fs {
				pg.Size += int64(f.zf.UncompressedSize64)
			}
			info.Groups = append(info.Groups, pg)
		}
	}
	info.Instances = []PackTarget{}
	for _, in := range listInstances() {
		if in.MCVersion == m.MC && in.Loader == m.Loader {
			info.Instances = append(info.Instances, PackTarget{in.ID, in.Name, in.LoaderVersion})
		}
	}
	return info, nil
}

func kindOrder(k string) int {
	return map[string]int{"mod": 0, "resourcepack": 1, "shader": 2}[k] + map[bool]int{true: 3}[k == "other"]
}

// ---------- importing ----------

type PackImportRequest struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	TargetID string   `json:"targetId"` // empty = new instance
	Entries  []string `json:"entries"`  // selected entry ids (nil = all)
	Groups   []string `json:"groups"`   // selected groups (nil = all)
	All      bool     `json:"all"`
}

// importModpack installs everything in a modpack file as a new instance.
func importModpack(j *Job, file string, ref ModpackRef, nameOverride string) (*ModpackResult, error) {
	return importPack(j, file, ref, PackImportRequest{Name: nameOverride, All: true})
}

func importPack(j *Job, file string, ref ModpackRef, req PackImportRequest) (*ModpackResult, error) {
	m, err := readPack(file)
	if err != nil {
		return nil, err
	}
	defer m.Close()
	selE, selG := map[string]bool{}, map[string]bool{}
	for _, id := range req.Entries {
		selE[id] = true
	}
	for _, id := range req.Groups {
		selG[id] = true
	}
	if ref.Name == "" {
		ref.Name = m.Name
	}
	if ref.Version == "" {
		ref.Version = m.Version
	}

	var in *Instance
	var res *ModpackResult
	existing := req.TargetID != ""
	if existing {
		in, err = loadInstance(req.TargetID)
		if err != nil {
			return nil, err
		}
		if in.MCVersion != m.MC || in.Loader != m.Loader {
			return nil, errf("„%s“ passt nicht: Das Modpack braucht %s %s.", in.Name, loaderNames[m.Loader], m.MC)
		}
		res = &ModpackResult{Instance: in}
		j.logf("Füge Inhalte von „%s“ zu „%s“ hinzu …", ref.Name, in.Name)
	} else {
		in, res, err = createPackInstance(j, ref, req.Name, m.MC, m.Loader, m.LV)
		if err != nil {
			return nil, err
		}
	}

	// for an existing instance: replace older versions of the same projects and make it undoable
	var snap *Snapshot
	var modTarget *Target
	if existing && in.Loader != "vanilla" {
		if modTarget, err = loadTarget("instance", in.ID); err == nil {
			snap = beginSnapshot(modTarget, L("Modpack-Inhalte hinzugefügt"))
		}
	}
	var added []string
	prepare := func(pf *packFile) error {
		if snap == nil || pf.Kind != "mod" {
			return nil
		}
		name := path.Base(pf.Rel)
		if pf.ProjectKey != "" {
			if old := modTarget.Items[pf.ProjectKey]; old != nil && old.DiskName() != name {
				if err := snap.moveOut(modTarget, old.DiskName()); err != nil {
					return err
				}
				delete(modTarget.Items, pf.ProjectKey)
			}
		}
		if fileExists(filepath.Join(modTarget.Dir, name)) {
			if err := snap.moveOut(modTarget, name); err != nil {
				return err
			}
		}
		snap.added(name)
		added = append(added, pf.Name)
		return nil
	}

	type task struct {
		pf   *packFile
		dest string
	}
	var tasks []task
	var fromZip []*packFile
	for _, pf := range m.Files {
		if !req.All && !selE[pf.ID] {
			continue
		}
		if pf.ManualURL == "-" {
			res.Failed = append(res.Failed, pf.Name+": "+L("Datei nicht gefunden"))
			continue
		}
		if err := prepare(pf); err != nil {
			return nil, err
		}
		if pf.ManualURL != "" {
			res.Manual = append(res.Manual, ManualFile{Name: pf.Name, FileName: path.Base(pf.Rel), URL: pf.ManualURL, Folder: filepath.Join(in.Dir, filepath.FromSlash(path.Dir(pf.Rel)))})
			continue
		}
		if len(pf.zfs) > 0 {
			fromZip = append(fromZip, pf)
			continue
		}
		dest, err := safeJoin(in.Dir, pf.Rel)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task{pf, dest})
	}
	res.Files = len(tasks) + len(fromZip) + len(res.Manual)
	res.Skipped = m.Skipped
	var mu sync.Mutex
	done := 0
	runParallel(len(tasks), 6, func(i int) {
		t := tasks[i]
		var lastErr error = errNew("keine Download-Adresse")
		for _, u := range t.pf.URLs {
			if lastErr = download(u, t.dest, t.pf.Hash, nil); lastErr == nil {
				break
			}
		}
		mu.Lock()
		done++
		j.setStep(sprintf("Lade Dateien des Modpacks … %d/%d", done, len(tasks)), 0.1+0.7*float64(done)/float64(max(1, len(tasks))))
		if lastErr != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", t.pf.Name, lastErr))
			j.logf("✗ %s: %v", t.pf.Name, lastErr)
		}
		mu.Unlock()
	})

	j.setStep("Kopiere Einstellungen des Modpacks …", 0.85)
	for _, pf := range fromZip {
		for _, zf := range pf.zfs {
			if err := extractZipFile(zf, in.Dir, strings.TrimPrefix(zf.Name, pf.zprefix)); err != nil {
				res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", pf.Name, err))
			}
		}
	}
	for _, g := range []string{"config", "options", "servers", "saves"} {
		if !req.All && !selG[g] {
			continue
		}
		n := 0
		for _, of := range m.Groups[g] {
			if g == "servers" && existing && fileExists(filepath.Join(in.Dir, "servers.dat")) {
				n += mergeServers(of.zf, in.Dir)
				continue
			}
			if err := extractZipFile(of.zf, in.Dir, of.rel); err != nil {
				return nil, err
			}
			n++
		}
		if n > 0 {
			j.logf("%d Datei(en) übernommen (%s).", n, L(groupLabel(g)))
		}
	}
	for _, mf := range res.Manual {
		j.logf("⚠ %s muss manuell geladen werden: %s", mf.Name, mf.URL)
	}
	if snap != nil {
		if len(added) > 0 {
			snap.commit(modTarget, sprintf("Aus Modpack: %s", strings.Join(added, ", ")))
		} else {
			snap.commit(modTarget, "")
		}
		modTarget.save()
		recheckLater(modTarget)
	}
	if existing {
		for _, typ := range []string{"instance-rp", "instance-shader"} {
			if t, err := loadTarget(typ, in.ID); err == nil {
				identifyTarget(j, t)
			}
		}
	}
	return finishPack(j, in, res)
}

func groupLabel(g string) string {
	return map[string]string{"config": "Mod-Einstellungen und weitere Dateien", "options": "Spieleinstellungen (Grafik, Tastenbelegung)",
		"servers": "Server-Liste", "saves": "Welten"}[g]
}

// extractZipFile writes one archive entry to base/rel.
func extractZipFile(f *zip.File, base, rel string) error {
	target, err := safeJoin(base, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(rc, 2<<30))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// mergeServers adds the servers of a pack's servers.dat to the existing list instead of replacing it.
func mergeServers(f *zip.File, gameDir string) int {
	rc, err := f.Open()
	if err != nil {
		return 0
	}
	b, _ := io.ReadAll(io.LimitReader(rc, 4<<20))
	rc.Close()
	n := 0
	for _, s := range readServerList(b) {
		if ok, _ := addServerToList(gameDir, s[0], s[1]); ok {
			n++
		}
	}
	return n
}

// readServerList returns name and address of every entry in a servers.dat.
func readServerList(b []byte) [][2]string {
	if len(b) < 3 || b[0] != 10 {
		return nil
	}
	r := bytes.NewReader(b[1:])
	var n uint16
	binary.Read(r, binary.BigEndian, &n)
	r.Seek(int64(n), io.SeekCurrent)
	v, err := nbtReadPayload(r, 10, 0)
	if err != nil {
		return nil
	}
	var out [][2]string
	for _, t := range v.([]*nbtTag) {
		list, ok := t.Value.(nbtList)
		if t.Name != "servers" || !ok {
			continue
		}
		for _, it := range list.Items {
			tags, _ := it.([]*nbtTag)
			var name, ip string
			for _, x := range tags {
				s, _ := x.Value.(string)
				switch x.Name {
				case "name":
					name = s
				case "ip":
					ip = s
				}
			}
			if ip != "" {
				out = append(out, [2]string{firstNonEmpty(name, ip), ip})
			}
		}
	}
	return out
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
