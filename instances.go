package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// InstalledItem is a mod or plugin that CraftKit installed.
type InstalledItem struct {
	Key           string   `json:"key"` // source:projectId
	Source        string   `json:"source"`
	ProjectID     string   `json:"projectId"`
	Slug          string   `json:"slug,omitempty"`
	Name          string   `json:"name"`
	IconURL       string   `json:"iconUrl,omitempty"`
	PageURL       string   `json:"pageUrl,omitempty"`
	VersionID     string   `json:"versionId"`
	VersionNumber string   `json:"versionNumber"`
	VersionDate   string   `json:"versionDate,omitempty"`
	FileName      string   `json:"fileName"` // name of the jar (without ".disabled")
	Disabled      bool     `json:"disabled,omitempty"`
	Explicit      bool     `json:"explicit"`     // chosen by the user (not only pulled in as dependency)
	Dependencies  []string `json:"dependencies"` // keys of required items
	Incompatible  []string `json:"incompatible,omitempty"`
	InstalledAt   string   `json:"installedAt"`
}

type Instance struct {
	ID            string                    `json:"id"`
	Name          string                    `json:"name"`
	MCVersion     string                    `json:"mcVersion"`
	Loader        string                    `json:"loader"`
	LoaderVersion string                    `json:"loaderVersion"`
	VersionID     string                    `json:"versionId"` // id in .minecraft/versions
	MemoryGB      int                       `json:"memoryGB"`
	Created       string                    `json:"created"`
	Items         map[string]*InstalledItem `json:"mods"`
	ProfileKey    string                    `json:"profileKey,omitempty"` // set for profiles taken over from the launcher
	Adopted       bool                      `json:"adopted,omitempty"`
	ServerAddress string                    `json:"serverAddress,omitempty"`
	Modpack       *ModpackRef               `json:"modpack,omitempty"`
	ResourcePacks map[string]*InstalledItem `json:"resourcePacks,omitempty"`
	Shaders       map[string]*InstalledItem `json:"shaders,omitempty"`
	Dir           string                    `json:"dir"`
}

const instanceManifest = "craftkit.json"
const pluginManifest = ".craftkit-plugins.json"

var instMu sync.Mutex // serialises manifest writes

func instanceDir(id string) string {
	if d, ok := getConfig().LinkedInstances[id]; ok {
		return d
	}
	return filepath.Join(getConfig().InstancesDir, id)
}

func instanceIDExists(id string) bool {
	if _, ok := getConfig().LinkedInstances[id]; ok {
		return true
	}
	return fileExists(filepath.Join(getConfig().InstancesDir, id))
}

func loadInstance(id string) (*Instance, error) {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return nil, fmt.Errorf("ungültige Instanz")
	}
	dir := instanceDir(id)
	b, err := os.ReadFile(filepath.Join(dir, instanceManifest))
	if err != nil {
		return nil, fmt.Errorf("Instanz %q nicht gefunden", id)
	}
	var in Instance
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	in.ID = id
	in.Dir = dir
	if in.Items == nil {
		in.Items = map[string]*InstalledItem{}
	}
	return &in, nil
}

func (in *Instance) save() error {
	return writeJSONFile(filepath.Join(in.Dir, instanceManifest), in)
}

func listInstances() []*Instance {
	c := getConfig()
	ents, _ := os.ReadDir(c.InstancesDir)
	var out []*Instance
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if _, linked := c.LinkedInstances[e.Name()]; linked {
			continue
		}
		if in, err := loadInstance(e.Name()); err == nil {
			out = append(out, in)
		}
	}
	for id := range c.LinkedInstances {
		if in, err := loadInstance(id); err == nil {
			out = append(out, in)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Created > out[k].Created })
	return out
}

// modLoadersFor returns the Modrinth loader tags whose mods run on this loader.
func modLoadersFor(loader string) []string {
	switch loader {
	case "fabric":
		return []string{"fabric"}
	case "quilt":
		return []string{"quilt", "fabric"}
	case "forge":
		return []string{"forge"}
	case "neoforge":
		return []string{"neoforge"}
	}
	return nil
}

// pluginLoadersFor returns plugin platform tags compatible with a server platform.
func pluginLoadersFor(platform string) []string {
	switch platform {
	case "paper":
		return []string{"paper", "spigot", "bukkit"}
	case "purpur":
		return []string{"purpur", "paper", "spigot", "bukkit"}
	case "spigot":
		return []string{"spigot", "bukkit"}
	case "bukkit":
		return []string{"bukkit"}
	case "folia":
		return []string{"folia"}
	case "velocity":
		return []string{"velocity"}
	case "bungeecord":
		return []string{"bungeecord", "waterfall"}
	case "waterfall":
		return []string{"waterfall", "bungeecord"}
	}
	return []string{platform}
}

// ---------- creating instances ----------

type CreateInstanceReq struct {
	Name          string `json:"name"`
	MCVersion     string `json:"mcVersion"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion"`
	MemoryGB      int    `json:"memoryGB"`
}

func createInstance(j *Job, req CreateInstanceReq) (*Instance, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = loaderNames[req.Loader] + " " + req.MCVersion
	}
	if _, ok := loaderNames[req.Loader]; !ok {
		return nil, fmt.Errorf("unbekannter Loader")
	}
	if req.MCVersion == "" {
		return nil, fmt.Errorf("keine Minecraft-Version gewählt")
	}
	if req.Loader != "vanilla" && req.LoaderVersion == "" {
		return nil, fmt.Errorf("keine %s-Version gewählt", loaderNames[req.Loader])
	}
	base := slugify(req.Name)
	id := base
	for n := 2; instanceIDExists(id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}

	j.setStep("Installiere "+loaderNames[req.Loader]+" …", -1)
	versionID, err := installLoader(j, req.Loader, req.MCVersion, req.LoaderVersion)
	if err != nil {
		return nil, err
	}

	in := &Instance{
		ID: id, Name: req.Name, MCVersion: req.MCVersion, Loader: req.Loader,
		LoaderVersion: req.LoaderVersion, VersionID: versionID, MemoryGB: req.MemoryGB,
		Created: time.Now().Format(time.RFC3339), Items: map[string]*InstalledItem{},
		Dir: instanceDir(id),
	}
	for _, sub := range []string{"mods", "config", "resourcepacks", "shaderpacks", "saves"} {
		if in.Loader == "vanilla" && (sub == "mods" || sub == "config") {
			continue
		}
		os.MkdirAll(filepath.Join(in.Dir, sub), 0o755)
	}
	instMu.Lock()
	err = in.save()
	instMu.Unlock()
	if err != nil {
		return nil, err
	}
	j.setStep("Trage Profil im Minecraft Launcher ein …", 0.95)
	if err := writeLauncherProfile(in); err != nil {
		return nil, fmt.Errorf("Profil konnte nicht eingetragen werden: %w", err)
	}
	j.logf("Profil „%s“ im Minecraft Launcher angelegt (Version %s).", in.Name, versionID)
	return in, nil
}

// installLoader makes sure the given version exists in .minecraft/versions and returns its id.
func installLoader(j *Job, loader, mc, lv string) (string, error) {
	mcDir := getConfig().MinecraftDir
	if err := os.MkdirAll(filepath.Join(mcDir, "versions"), 0o755); err != nil {
		return "", err
	}
	switch loader {
	case "vanilla":
		j.logf("Vanilla %s – der Minecraft Launcher lädt die Spieldateien beim ersten Start.", mc)
		return mc, nil
	case "fabric", "quilt":
		base := fabricMeta
		if loader == "quilt" {
			base = quiltMeta
		}
		u := fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", base, url.PathEscape(mc), url.PathEscape(lv))
		j.logf("Lade %s-Profil %s für Minecraft %s …", loaderNames[loader], lv, mc)
		b, err := getBytes(u, nil, 0)
		if err != nil {
			return "", err
		}
		var meta struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(b, &meta); err != nil || meta.ID == "" {
			return "", fmt.Errorf("ungültiges %s-Profil", loaderNames[loader])
		}
		vdir := filepath.Join(mcDir, "versions", meta.ID)
		if err := os.MkdirAll(vdir, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(vdir, meta.ID+".json"), b, 0o644); err != nil {
			return "", err
		}
		// empty jar keeps older launchers happy (same as the official installers do)
		jar := filepath.Join(vdir, meta.ID+".jar")
		if !fileExists(jar) {
			os.WriteFile(jar, nil, 0o644)
		}
		j.logf("Version %s installiert.", meta.ID)
		return meta.ID, nil
	case "forge", "neoforge":
		return installForgeLike(j, loader, mc, lv)
	}
	return "", fmt.Errorf("unbekannter Loader")
}

func listVersionDirs(mcDir string) map[string]bool {
	set := map[string]bool{}
	ents, _ := os.ReadDir(filepath.Join(mcDir, "versions"))
	for _, e := range ents {
		if e.IsDir() && fileExists(filepath.Join(mcDir, "versions", e.Name(), e.Name()+".json")) {
			set[e.Name()] = true
		}
	}
	return set
}

func installForgeLike(j *Job, loader, mc, lv string) (string, error) {
	mcDir := getConfig().MinecraftDir
	var expected []string
	var installerURL string
	if loader == "forge" {
		short := forgeShort(lv)
		expected = []string{mc + "-forge-" + short, mc + "-forge" + lv, mc + "-Forge" + short + "-" + mc}
		installerURL = fmt.Sprintf("%s/%s/forge-%s-installer.jar", forgeMaven, lv, lv)
	} else {
		expected = []string{"neoforge-" + lv}
		installerURL = fmt.Sprintf("%s/%s/neoforge-%s-installer.jar", neoforgeMaven, lv, lv)
	}
	before := listVersionDirs(mcDir)
	for _, e := range expected {
		if before[e] {
			j.logf("%s ist bereits installiert (%s).", loaderNames[loader], e)
			return e, nil
		}
	}

	java, err := ensureJava(j)
	if err != nil {
		return "", err
	}
	// installers refuse to run without a launcher_profiles.json
	ensureLauncherProfilesFile(mcDir)

	tmpDir := filepath.Join(dataDir(), "tmp")
	os.MkdirAll(tmpDir, 0o755)
	installer := filepath.Join(tmpDir, filepath.Base(installerURL))
	j.logf("Lade %s-Installer %s …", loaderNames[loader], lv)
	if err := download(installerURL, installer, nil, func(done, total int64) {
		if total > 0 {
			j.setStep(fmt.Sprintf("Lade Installer … %d%%", done*100/total), float64(done)/float64(total)*0.2)
		}
	}); err != nil {
		return "", fmt.Errorf("Installer-Download fehlgeschlagen: %w", err)
	}
	defer os.Remove(installer)

	j.setStep(loaderNames[loader]+" wird installiert (lädt Bibliotheken, kann einige Minuten dauern) …", -1)
	flags := [][]string{{"--installClient", mcDir}, {"--install-client", mcDir}}
	if loader == "neoforge" {
		flags = [][]string{{"--install-client", mcDir}, {"--installClient", mcDir}}
	}
	var runErr error
	for _, f := range flags {
		runErr = runJava(j, java, tmpDir, append([]string{"-jar", installer}, f...))
		if runErr == nil {
			break
		}
		if !strings.Contains(runErr.Error(), "recognized") && !strings.Contains(runErr.Error(), "Unrecognized") {
			// real failure, not an unknown option – still try the next flag variant once
			j.logf("Installer-Versuch fehlgeschlagen: %v", runErr)
		}
	}
	if newID := findNewVersion(mcDir, before, expected, loader); newID != "" {
		j.logf("%s installiert: %s", loaderNames[loader], newID)
		return newID, nil
	}

	// Very old installers have no headless mode – fall back to the installer window.
	j.logf("Automatische Installation nicht möglich – öffne das Installer-Fenster.")
	j.setStep("Bitte im Installer-Fenster „Install client“ wählen und bestätigen …", -1)
	javaw := strings.TrimSuffix(java, "java.exe") + "javaw.exe"
	if !fileExists(javaw) {
		javaw = java
	}
	cmd := exec.Command(javaw, "-jar", installer)
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		j.logf("Installer beendet: %v", err)
	}
	if newID := findNewVersion(mcDir, before, expected, loader); newID != "" {
		j.logf("%s installiert: %s", loaderNames[loader], newID)
		return newID, nil
	}
	if runErr != nil {
		return "", fmt.Errorf("%s-Installation fehlgeschlagen: %w", loaderNames[loader], runErr)
	}
	return "", fmt.Errorf("%s-Installation wurde nicht abgeschlossen", loaderNames[loader])
}

func findNewVersion(mcDir string, before map[string]bool, expected []string, loader string) string {
	after := listVersionDirs(mcDir)
	for _, e := range expected {
		if after[e] {
			return e
		}
	}
	for id := range after {
		if !before[id] && strings.Contains(strings.ToLower(id), loader) {
			return id
		}
	}
	return ""
}

func runJava(j *Job, java, dir string, args []string) error {
	cmd := exec.Command(java, args...)
	cmd.Dir = dir
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	var tail []string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		n++
		tail = append(tail, line)
		if len(tail) > 8 {
			tail = tail[1:]
		}
		// keep the UI log readable: show every 10th line plus important ones
		low := strings.ToLower(line)
		if n%10 == 0 || strings.Contains(low, "error") || strings.Contains(low, "exception") || strings.Contains(low, "success") || strings.Contains(low, "processor") {
			j.logf("  %s", line)
		}
		j.setStep("Installer: "+truncate(line, 90), -1)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%v – %s", err, strings.Join(tail, " | "))
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ---------- launcher_profiles.json ----------

func launcherProfileFiles(mcDir string) []string {
	var files []string
	for _, n := range []string{"launcher_profiles.json", "launcher_profiles_microsoft_store.json"} {
		p := filepath.Join(mcDir, n)
		if fileExists(p) {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		files = append(files, ensureLauncherProfilesFile(mcDir))
	}
	return files
}

func ensureLauncherProfilesFile(mcDir string) string {
	p := filepath.Join(mcDir, "launcher_profiles.json")
	if !fileExists(p) {
		os.MkdirAll(mcDir, 0o755)
		writeJSONFile(p, map[string]any{"profiles": map[string]any{}, "version": 3})
	}
	return p
}

var profileIcons = map[string]string{
	"vanilla": "Grass", "forge": "Anvil", "neoforge": "Furnace", "fabric": "Crafting_Table", "quilt": "Bookshelf",
}

func profileKey(in *Instance) string {
	if in.ProfileKey != "" {
		return in.ProfileKey
	}
	return "craftkit-" + in.ID
}

func editProfiles(fn func(profiles map[string]any) error) error {
	mcDir := getConfig().MinecraftDir
	for _, f := range launcherProfileFiles(mcDir) {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		root := map[string]any{}
		if len(strings.TrimSpace(string(b))) > 0 {
			if err := json.Unmarshal(b, &root); err != nil {
				return fmt.Errorf("%s ist beschädigt: %w", filepath.Base(f), err)
			}
		}
		profiles, _ := root["profiles"].(map[string]any)
		if profiles == nil {
			profiles = map[string]any{}
		}
		if err := fn(profiles); err != nil {
			return err
		}
		root["profiles"] = profiles
		// backup once per run of the program
		bak := f + ".craftkit-backup"
		if !fileExists(bak) {
			os.WriteFile(bak, b, 0o644)
		}
		if err := writeJSONFile(f, root); err != nil {
			return err
		}
	}
	return nil
}

func writeLauncherProfile(in *Instance) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	return editProfiles(func(profiles map[string]any) error {
		key := profileKey(in)
		p, _ := profiles[key].(map[string]any)
		if in.Adopted {
			// profile belongs to the user – only touch what CraftKit manages
			if p == nil {
				return nil
			}
			if in.MemoryGB > 0 {
				p["javaArgs"] = fmt.Sprintf("-Xmx%dG -XX:+UnlockExperimentalVMOptions -XX:+UseG1GC -XX:G1NewSizePercent=20 -XX:G1ReservePercent=20 -XX:MaxGCPauseMillis=50 -XX:G1HeapRegionSize=32M", in.MemoryGB)
			}
			if in.VersionID != "" {
				p["lastVersionId"] = in.VersionID
			}
			profiles[key] = p
			return nil
		}
		if p == nil {
			p = map[string]any{"created": now}
		}
		p["name"] = in.Name + " (CraftKit)"
		p["type"] = "custom"
		p["lastVersionId"] = in.VersionID
		p["gameDir"] = in.Dir
		p["icon"] = profileIcons[in.Loader]
		p["lastUsed"] = now
		if in.MemoryGB > 0 {
			p["javaArgs"] = fmt.Sprintf("-Xmx%dG -XX:+UnlockExperimentalVMOptions -XX:+UseG1GC -XX:G1NewSizePercent=20 -XX:G1ReservePercent=20 -XX:MaxGCPauseMillis=50 -XX:G1HeapRegionSize=32M", in.MemoryGB)
		} else {
			delete(p, "javaArgs")
		}
		profiles[key] = p
		return nil
	})
}

func removeLauncherProfile(in *Instance) error {
	return editProfiles(func(profiles map[string]any) error {
		delete(profiles, profileKey(in))
		return nil
	})
}

func deleteInstance(id string, deleteFiles bool) error {
	in, err := loadInstance(id)
	if err != nil {
		return err
	}
	if in.Adopted {
		// taken over from the launcher: only forget it, never delete the user's profile or files
		os.Rename(filepath.Join(in.Dir, instanceManifest), filepath.Join(in.Dir, instanceManifest+".removed"))
		return updateConfig(func(c *Config) { delete(c.LinkedInstances, id) })
	}
	if err := removeLauncherProfile(in); err != nil {
		return err
	}
	if deleteFiles {
		abs, _ := filepath.Abs(in.Dir)
		mc, _ := filepath.Abs(getConfig().MinecraftDir)
		if strings.EqualFold(abs, mc) || len(abs) < 8 {
			return errors.New("dieser Ordner wird aus Sicherheitsgründen nicht gelöscht")
		}
		return os.RemoveAll(in.Dir)
	}
	// keep files but hide from CraftKit
	return os.Rename(filepath.Join(in.Dir, instanceManifest), filepath.Join(in.Dir, instanceManifest+".removed"))
}

func updateInstanceSettings(id, name string, memoryGB int) (*Instance, error) {
	instMu.Lock()
	defer instMu.Unlock()
	in, err := loadInstance(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) != "" {
		in.Name = strings.TrimSpace(name)
	}
	if memoryGB < 0 || memoryGB > 64 {
		return nil, errors.New("ungültige RAM-Angabe")
	}
	in.MemoryGB = memoryGB
	if err := in.save(); err != nil {
		return nil, err
	}
	return in, writeLauncherProfile(in)
}

// foreignFiles lists jars in the content folder that CraftKit does not manage.
func foreignFiles(dir string, items map[string]*InstalledItem) []string {
	known := map[string]bool{}
	for _, it := range items {
		known[strings.ToLower(it.FileName)] = true
		known[strings.ToLower(it.FileName+".disabled")] = true
	}
	ents, _ := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		n := e.Name()
		ln := strings.ToLower(n)
		if e.IsDir() || !(strings.HasSuffix(ln, ".jar") || strings.HasSuffix(ln, ".disabled")) {
			continue
		}
		if !known[ln] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// DiskName is the file name as it currently exists in the folder.
func (it *InstalledItem) DiskName() string {
	if it.Disabled {
		return it.FileName + ".disabled"
	}
	return it.FileName
}

// launcherProfileName is the name the instance has in the official launcher.
func launcherProfileName(in *Instance) string {
	if in.Adopted {
		return in.Name
	}
	return in.Name + " (CraftKit)"
}

// touchProfile marks the profile as last used so the launcher lists it first.
func touchProfile(in *Instance) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	return editProfiles(func(profiles map[string]any) error {
		if p, ok := profiles[profileKey(in)].(map[string]any); ok {
			p["lastUsed"] = now
		}
		return nil
	})
}

// PackFile is a resource pack or shader pack in the folder that CraftKit does not manage.
type PackFile struct {
	File     string `json:"file"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
	IsDir    bool   `json:"isDir"`
}

func foreignPacks(t *Target) []PackFile {
	known := map[string]bool{}
	for _, it := range t.Items {
		known[strings.ToLower(it.FileName)] = true
		known[strings.ToLower(it.DiskName())] = true
	}
	ents, _ := os.ReadDir(t.Dir)
	var out []PackFile
	for _, e := range ents {
		n := e.Name()
		ln := strings.ToLower(n)
		if known[ln] || strings.HasPrefix(n, ".") || strings.HasSuffix(ln, ".txt") {
			continue
		}
		if !e.IsDir() && !strings.HasSuffix(ln, ".zip") && !strings.HasSuffix(ln, ".zip.disabled") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimSuffix(n, ".disabled"), ".zip")
		out = append(out, PackFile{File: n, Name: name, Disabled: strings.HasSuffix(ln, ".disabled"), IsDir: e.IsDir()})
	}
	sort.Slice(out, func(i, k int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[k].Name) })
	return out
}
