package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

const preferredPort = 27580

var (
	token    string
	logger   *log.Logger
	lastPing = time.Now()
	pingMu   sync.Mutex
	byeAt    time.Time
)

func logf(format string, a ...any) {
	if logger != nil {
		logger.Printf(format, a...)
	}
}

func main() {
	noWindow := flag.Bool("no-window", false, "UI nicht automatisch öffnen")
	port := flag.Int("port", preferredPort, "Port")
	flag.Parse()

	os.MkdirAll(dataDir(), 0o755)
	lf, err := os.OpenFile(filepath.Join(dataDir(), "craftkit.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err == nil {
		logger = log.New(lf, "", log.LstdFlags)
	} else {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	loadConfig()
	logf("CraftKit %s startet, Daten: %s", appVersion, dataDir())

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		// already running? then just show its window
		addr := fmt.Sprintf("http://127.0.0.1:%d", *port)
		c := &http.Client{Timeout: 2 * time.Second}
		if resp, err2 := c.Get(addr + "/api/hello"); err2 == nil {
			resp.Body.Close()
			if resp.Header.Get("X-App") == appName {
				openAppWindow(addr + "/")
				return
			}
		}
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			logf("kein Port frei: %v", err)
			os.Exit(1)
		}
	}
	b := make([]byte, 16)
	rand.Read(b)
	token = hex.EncodeToString(b)
	url := fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)
	logf("UI: %s", url)
	fmt.Println("CraftKit läuft auf", url)

	mux := http.NewServeMux()
	registerRoutes(mux)
	srv := &http.Server{Handler: guard(mux), ReadHeaderTimeout: 10 * time.Second}

	if !*noWindow {
		go func() {
			time.Sleep(200 * time.Millisecond)
			if err := openAppWindow(url); err != nil {
				logf("Fenster öffnen fehlgeschlagen: %v", err)
			}
		}()
		go watchdog()
	}
	if err := srv.Serve(ln); err != nil {
		logf("server: %v", err)
	}
}

// watchdog exits the program once the UI window has been closed.
func watchdog() {
	for {
		time.Sleep(3 * time.Second)
		pingMu.Lock()
		idle := time.Since(lastPing)
		bye := !byeAt.IsZero() && time.Since(byeAt) > 8*time.Second && lastPing.Before(byeAt)
		pingMu.Unlock()
		if anyJobRunning() {
			continue
		}
		// background windows ping less often (browser throttling), so be generous
		if bye || idle > 4*time.Minute {
			logf("Fenster geschlossen – beende.")
			os.Exit(0)
		}
	}
}

// guard blocks requests from other websites (DNS rebinding / CSRF).
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/hello" {
			if r.Header.Get("X-CraftKit-Token") != token && r.URL.Query().Get("t") != token {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

type handlerFunc func(r *http.Request) (any, error)

func api(fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := fn(r)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, res)
	}
}

func registerRoutes(mux *http.ServeMux) {
	sub, _ := fs.Sub(webFS, "web")
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			b, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write([]byte(strings.Replace(string(b), "__TOKEN__", token, 1)))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		static.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-App", appName)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		pingMu.Lock()
		lastPing = time.Now()
		pingMu.Unlock()
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/bye", func(w http.ResponseWriter, r *http.Request) {
		pingMu.Lock()
		byeAt = time.Now()
		pingMu.Unlock()
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/api/state", api(func(r *http.Request) (any, error) {
		c := getConfig()
		label, target := detectLauncher()
		type instSummary struct {
			*Instance
			ModCount int `json:"modCount"`
		}
		var insts []instSummary
		for _, in := range listInstances() {
			insts = append(insts, instSummary{in, len(in.Items)})
		}
		pfs := c.PluginFolders
		type pfSummary struct {
			PluginFolder
			Count  int  `json:"count"`
			Exists bool `json:"exists"`
		}
		var pfOut []pfSummary
		for _, f := range pfs {
			s := pfSummary{PluginFolder: f, Exists: fileExists(f.Path)}
			if t, err := loadTarget("plugins", f.ID); err == nil {
				s.Count = len(t.Items)
			}
			pfOut = append(pfOut, s)
		}
		return map[string]any{
			"version":         appVersion,
			"config":          c,
			"instances":       insts,
			"pluginFolders":   pfOut,
			"launcherLabel":   label,
			"launcherTarget":  target,
			"launcherRunning": isProcessRunning("MinecraftLauncher.exe", "Minecraft.exe"),
			"java":            findJava(),
			"dataDir":         dataDir(),
			"minecraftFound":  fileExists(c.MinecraftDir),
		}, nil
	}))

	mux.HandleFunc("/api/game-versions", api(func(r *http.Request) (any, error) {
		return gameVersionsFor(r.URL.Query().Get("loader"), r.URL.Query().Get("snapshots") == "1")
	}))
	mux.HandleFunc("/api/loader-versions", api(func(r *http.Request) (any, error) {
		return loaderVersionsFor(r.URL.Query().Get("loader"), r.URL.Query().Get("mc"))
	}))

	mux.HandleFunc("/api/instances/create", api(func(r *http.Request) (any, error) {
		var req CreateInstanceReq
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		j := startJob("Instanz „"+req.Name+"“ anlegen", func(j *Job) (any, error) {
			return createInstance(j, req)
		})
		return map[string]string{"job": j.ID}, nil
	}))
	mux.HandleFunc("/api/instances/delete", api(func(r *http.Request) (any, error) {
		var req struct {
			ID          string `json:"id"`
			DeleteFiles bool   `json:"deleteFiles"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, deleteInstance(req.ID, req.DeleteFiles)
	}))
	mux.HandleFunc("/api/instances/settings", api(func(r *http.Request) (any, error) {
		var req struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			MemoryGB int    `json:"memoryGB"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		return updateInstanceSettings(req.ID, req.Name, req.MemoryGB)
	}))
	mux.HandleFunc("/api/instances/reprofile", api(func(r *http.Request) (any, error) {
		var req struct {
			ID string `json:"id"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		in, err := loadInstance(req.ID)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, writeLauncherProfile(in)
	}))

	mux.HandleFunc("/api/target", api(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		t, err := loadTarget(q.Get("type"), q.Get("id"))
		if err != nil {
			return nil, err
		}
		items := make([]*InstalledItem, 0, len(t.Items))
		for _, it := range t.Items {
			items = append(items, it)
		}
		out := map[string]any{"target": t, "items": items, "foreign": foreignFiles(t.Dir, t.Items)}
		var foreignInfo []*JarInfo
		for _, ji := range scanFolder(t.Dir) {
			if t.Items != nil {
				managed := false
				for _, it := range t.Items {
					if strings.EqualFold(it.FileName, ji.File) {
						managed = true
					}
				}
				if managed {
					continue
				}
			}
			foreignInfo = append(foreignInfo, ji)
		}
		out["foreignInfo"] = foreignInfo
		if t.Type == "instance" {
			in, _ := loadInstance(t.ID)
			out["instance"] = in
		}
		return out, nil
	}))

	mux.HandleFunc("/api/search", api(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		t, err := loadTarget(q.Get("type"), q.Get("id"))
		if err != nil {
			return nil, err
		}
		p, err := providerFor(q.Get("source"))
		if err != nil {
			return nil, err
		}
		off, _ := strconv.Atoi(q.Get("offset"))
		sq := SearchQuery{Query: strings.TrimSpace(q.Get("q")), Kind: t.Kind, MCVersion: t.MCVersion, Loaders: t.Loaders, Offset: off, Limit: 20}
		if t.Kind == "plugin" {
			sq.MCVersion = "" // plugins are rarely tagged per version, filter at install time
		}
		hits, total, err := p.Search(sq)
		if err != nil {
			return nil, err
		}
		type hit struct {
			Project
			Installed bool `json:"installed"`
		}
		out := make([]hit, 0, len(hits))
		for _, h := range hits {
			out = append(out, hit{h, t.Items[itemKey(h.Source, h.ID)] != nil})
		}
		return map[string]any{"hits": out, "total": total}, nil
	}))

	mux.HandleFunc("/api/versions", api(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		t, err := loadTarget(q.Get("type"), q.Get("id"))
		if err != nil {
			return nil, err
		}
		p, err := providerFor(q.Get("source"))
		if err != nil {
			return nil, err
		}
		vs, err := p.Versions(q.Get("project"), t.Kind, t.MCVersion, t.Loaders)
		if err != nil {
			return nil, err
		}
		var ok []ModVersion
		for _, v := range vs {
			if compatible(t, &v) {
				ok = append(ok, v)
			}
		}
		if len(ok) > 40 {
			ok = ok[:40]
		}
		return ok, nil
	}))

	mux.HandleFunc("/api/plan", api(func(r *http.Request) (any, error) {
		var req struct {
			Type      string        `json:"type"`
			ID        string        `json:"id"`
			Requests  []PlanRequest `json:"requests"`
			UpdateAll bool          `json:"updateAll"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		t, err := loadTarget(req.Type, req.ID)
		if err != nil {
			return nil, err
		}
		reqs := req.Requests
		if req.UpdateAll {
			for _, it := range t.Items {
				reqs = append(reqs, PlanRequest{Source: it.Source, ProjectID: it.ProjectID})
			}
		}
		if len(reqs) == 0 {
			return nil, fmt.Errorf("nichts ausgewählt")
		}
		if req.UpdateAll {
			// during "update all" every installed item is checked, not only explicit ones
			res := newResolver().resolveUpdateAll(t, reqs)
			storePlan(res)
			return res, nil
		}
		plan := newResolver().resolve(t, reqs)
		storePlan(plan)
		return plan, nil
	}))

	mux.HandleFunc("/api/apply", api(func(r *http.Request) (any, error) {
		var req struct {
			PlanID string `json:"planId"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		plan := takePlan(req.PlanID)
		if plan == nil {
			return nil, fmt.Errorf("Plan abgelaufen – bitte erneut prüfen")
		}
		j := startJob("Installation in „"+plan.Target.Name+"“", func(j *Job) (any, error) {
			return applyPlan(j, plan)
		})
		return map[string]string{"job": j.ID}, nil
	}))

	mux.HandleFunc("/api/remove-check", api(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		t, err := loadTarget(q.Get("type"), q.Get("id"))
		if err != nil {
			return nil, err
		}
		key := q.Get("key")
		var orphanNames []string
		for _, o := range orphansAfterRemoval(t, key) {
			orphanNames = append(orphanNames, o.Name)
		}
		return map[string]any{"dependents": dependents(t, key), "orphans": orphanNames}, nil
	}))
	mux.HandleFunc("/api/remove", api(func(r *http.Request) (any, error) {
		var req struct {
			Type        string `json:"type"`
			ID          string `json:"id"`
			Key         string `json:"key"`
			WithOrphans bool   `json:"withOrphans"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		t, err := loadTarget(req.Type, req.ID)
		if err != nil {
			return nil, err
		}
		removed, err := removeItem(t, req.Key, req.WithOrphans)
		return map[string]any{"removed": removed}, err
	}))
	mux.HandleFunc("/api/remove-foreign", api(func(r *http.Request) (any, error) {
		var req struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			File string `json:"file"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		t, err := loadTarget(req.Type, req.ID)
		if err != nil {
			return nil, err
		}
		name := safeFileName(req.File)
		if name != req.File {
			return nil, fmt.Errorf("ungültiger Dateiname")
		}
		return map[string]bool{"ok": true}, os.Remove(filepath.Join(t.Dir, name))
	}))

	mux.HandleFunc("/api/plugin-folders/add", api(func(r *http.Request) (any, error) {
		var req PluginFolder
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		req.Path = strings.TrimSpace(req.Path)
		if req.Path == "" {
			return nil, fmt.Errorf("kein Ordner gewählt")
		}
		if req.Platform == "" {
			req.Platform = "paper"
		}
		if strings.TrimSpace(req.Name) == "" {
			req.Name = filepath.Base(req.Path)
		}
		b := make([]byte, 4)
		rand.Read(b)
		req.ID = hex.EncodeToString(b)
		if err := os.MkdirAll(req.Path, 0o755); err != nil {
			return nil, err
		}
		return req, updateConfig(func(c *Config) { c.PluginFolders = append(c.PluginFolders, req) })
	}))
	mux.HandleFunc("/api/plugin-folders/update", api(func(r *http.Request) (any, error) {
		var req PluginFolder
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		return req, updateConfig(func(c *Config) {
			for i := range c.PluginFolders {
				if c.PluginFolders[i].ID == req.ID {
					c.PluginFolders[i].Name = req.Name
					c.PluginFolders[i].Platform = req.Platform
					c.PluginFolders[i].MCVersion = req.MCVersion
				}
			}
		})
	}))
	mux.HandleFunc("/api/plugin-folders/delete", api(func(r *http.Request) (any, error) {
		var req struct {
			ID string `json:"id"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, updateConfig(func(c *Config) {
			var keep []PluginFolder
			for _, f := range c.PluginFolders {
				if f.ID != req.ID {
					keep = append(keep, f)
				}
			}
			c.PluginFolders = keep
		})
	}))

	mux.HandleFunc("/api/pick-folder", api(func(r *http.Request) (any, error) {
		var req struct {
			Title string `json:"title"`
		}
		readBody(r, &req)
		if req.Title == "" {
			req.Title = "Ordner wählen"
		}
		p, err := pickFolder(req.Title)
		return map[string]string{"path": p}, err
	}))
	mux.HandleFunc("/api/open-launcher", api(func(r *http.Request) (any, error) {
		return map[string]bool{"ok": true}, startLauncher()
	}))
	mux.HandleFunc("/api/open-folder", api(func(r *http.Request) (any, error) {
		var req struct {
			Path string `json:"path"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		os.MkdirAll(req.Path, 0o755)
		return map[string]bool{"ok": true}, openPath(req.Path)
	}))
	mux.HandleFunc("/api/open-url", api(func(r *http.Request) (any, error) {
		var req struct {
			URL string `json:"url"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(req.URL, "https://") {
			return nil, fmt.Errorf("nur https-Links")
		}
		return map[string]bool{"ok": true}, openBrowser(req.URL)
	}))
	mux.HandleFunc("/api/config", api(func(r *http.Request) (any, error) {
		var req Config
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		err := updateConfig(func(c *Config) {
			if strings.TrimSpace(req.MinecraftDir) != "" {
				c.MinecraftDir = strings.TrimSpace(req.MinecraftDir)
			}
			if strings.TrimSpace(req.InstancesDir) != "" {
				c.InstancesDir = strings.TrimSpace(req.InstancesDir)
			}
			c.LauncherPath = strings.TrimSpace(req.LauncherPath)
			c.CurseForgeKey = strings.TrimSpace(req.CurseForgeKey)
			c.JavaPath = strings.TrimSpace(req.JavaPath)
			c.ShowSnapshots = req.ShowSnapshots
		})
		return getConfig(), err
	}))
	mux.HandleFunc("/api/job", api(func(r *http.Request) (any, error) {
		j := getJob(r.URL.Query().Get("id"))
		if j == nil {
			return nil, fmt.Errorf("Auftrag nicht gefunden")
		}
		return j.snapshot(), nil
	}))

	// ---------- already installed things ----------
	mux.HandleFunc("/api/discover", api(func(r *http.Request) (any, error) {
		return map[string]any{"profiles": discoverProfiles(), "versions": installedVersions()}, nil
	}))
	mux.HandleFunc("/api/profile-scan", api(func(r *http.Request) (any, error) {
		key := r.URL.Query().Get("key")
		for _, p := range discoverProfiles() {
			if p.Key == key {
				jars := scanFolder(filepath.Join(p.GameDir, "mods"))
				miss := missingDeps(jars, "mod")
				suggestForMissing(miss)
				return map[string]any{"profile": p, "jars": jars, "missing": miss}, nil
			}
		}
		return nil, fmt.Errorf("Profil nicht gefunden")
	}))
	mux.HandleFunc("/api/adopt", api(func(r *http.Request) (any, error) {
		var req struct {
			Key string `json:"key"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		j := startJob("Profil übernehmen", func(j *Job) (any, error) { return adoptProfile(j, req.Key) })
		return map[string]string{"job": j.ID}, nil
	}))
	mux.HandleFunc("/api/identify", api(func(r *http.Request) (any, error) {
		var req struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		t, err := loadTarget(req.Type, req.ID)
		if err != nil {
			return nil, err
		}
		j := startJob("Dateien erkennen", func(j *Job) (any, error) { return identifyTarget(j, t) })
		return map[string]string{"job": j.ID}, nil
	}))
	mux.HandleFunc("/api/missing", api(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		t, err := loadTarget(q.Get("type"), q.Get("id"))
		if err != nil {
			return nil, err
		}
		miss := missingDeps(scanFolder(t.Dir), t.Kind)
		suggestForMissing(miss)
		// something CraftKit already installed is never "missing" (e.g. unreadable metadata)
		var out []*MissingDep
		for _, m := range miss {
			if m.ProjectID != "" && t.Items[itemKey(m.Source, m.ProjectID)] != nil {
				continue
			}
			out = append(out, m)
		}
		return out, nil
	}))

	// ---------- servers ----------
	mux.HandleFunc("/api/server/ping", api(func(r *http.Request) (any, error) {
		var req struct {
			Address string `json:"address"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		return pingServer(req.Address)
	}))
	mux.HandleFunc("/api/server/adapt", api(func(r *http.Request) (any, error) {
		var req AdaptRequest
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		j := startJob("An Server anpassen", func(j *Job) (any, error) { return adaptToServer(j, req) })
		return map[string]string{"job": j.ID}, nil
	}))
	mux.HandleFunc("/api/server/link", api(func(r *http.Request) (any, error) {
		var req struct {
			ID         string   `json:"id"`
			Address    string   `json:"address"`
			Name       string   `json:"name"`
			ServerMods []string `json:"serverMods"`
		}
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		in, added, err := linkServer(req.ID, req.Address, req.Name)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"instance": in, "addedToList": added}
		if len(req.ServerMods) > 0 {
			t, err := loadTarget("instance", in.ID)
			if err != nil {
				return nil, err
			}
			reqs, unresolved := serverModRequests(req.ServerMods)
			plan := &Plan{Target: t}
			if len(reqs) > 0 {
				plan = newResolver().resolve(t, reqs)
			}
			for _, u := range unresolved {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("Server-Mod „%s“ wurde nicht automatisch gefunden – bitte von Hand suchen.", u))
			}
			storePlan(plan)
			out["plan"] = plan
		}
		return out, nil
	}))
}
