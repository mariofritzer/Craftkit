package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// Config is stored in <dataDir>/config.json
type Config struct {
	MinecraftDir  string         `json:"minecraftDir"`
	InstancesDir  string         `json:"instancesDir"`
	LauncherPath  string         `json:"launcherPath"`  // empty = auto detect
	CurseForgeKey string         `json:"curseforgeKey"` // optional
	JavaPath      string         `json:"javaPath"`      // empty = auto
	ShowSnapshots bool           `json:"showSnapshots"`
	PluginFolders []PluginFolder `json:"pluginFolders"`
	// instances that live outside InstancesDir (profiles taken over from the launcher): id -> folder
	LinkedInstances map[string]string `json:"linkedInstances"`
}

type PluginFolder struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Platform  string `json:"platform"`  // paper, spigot, bukkit, purpur, folia, velocity, bungeecord
	MCVersion string `json:"mcVersion"` // empty = any
}

var (
	cfgMu sync.Mutex
	cfg   Config
)

func dataDir() string {
	if runtime.GOOS == "windows" {
		if a := os.Getenv("APPDATA"); a != "" {
			return filepath.Join(a, appName)
		}
	}
	h, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(h, "Library", "Application Support", appName)
	}
	return filepath.Join(h, "."+strings.ToLower(appName))
}

func defaultMinecraftDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), ".minecraft")
	case "darwin":
		h, _ := os.UserHomeDir()
		return filepath.Join(h, "Library", "Application Support", "minecraft")
	default:
		h, _ := os.UserHomeDir()
		return filepath.Join(h, ".minecraft")
	}
}

func loadConfig() {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	b, err := os.ReadFile(filepath.Join(dataDir(), "config.json"))
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.MinecraftDir == "" {
		cfg.MinecraftDir = defaultMinecraftDir()
	}
	if cfg.InstancesDir == "" {
		cfg.InstancesDir = filepath.Join(cfg.MinecraftDir, "craftkit-instances")
	}
	if cfg.CurseForgeKey == "" {
		cfg.CurseForgeKey = defaultCurseForgeKey
	}
}

// defaultCurseForgeKey can be set at build time:
//
//	go build -ldflags "-X 'main.defaultCurseForgeKey=...'"
var defaultCurseForgeKey = ""

func saveConfig() error {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return writeJSONFile(filepath.Join(dataDir(), "config.json"), cfg)
}

func getConfig() Config {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	c := cfg
	c.PluginFolders = append([]PluginFolder(nil), cfg.PluginFolders...)
	c.LinkedInstances = map[string]string{}
	for k, v := range cfg.LinkedInstances {
		c.LinkedInstances[k] = v
	}
	return c
}

func updateConfig(fn func(c *Config)) error {
	cfgMu.Lock()
	fn(&cfg)
	cfgMu.Unlock()
	return saveConfig()
}

func writeJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	r := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
	s = r.Replace(s)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "instanz"
	}
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// safeFileName strips path components and characters Windows does not allow.
func safeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	bad := `<>:"/\|?*`
	var b strings.Builder
	for _, r := range name {
		if r < 32 || strings.ContainsRune(bad, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		out = "datei.jar"
	}
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
