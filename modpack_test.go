package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withTempConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfgMu.Lock()
	old := cfg
	cfg = Config{MinecraftDir: filepath.Join(dir, "mc"), InstancesDir: filepath.Join(dir, "mc", "inst"), LinkedInstances: map[string]string{}}
	cfgMu.Unlock()
	t.Cleanup(func() { cfgMu.Lock(); cfg = old; cfgMu.Unlock() })
}

func TestImportMrpack(t *testing.T) {
	withTempConfig(t)
	jarA := []byte("jar-a-content")
	h := sha1.Sum(jarA)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a.jar" {
			w.Write(jarA)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	// the test server is http; the importer only accepts https URLs, so rewrite via a tiny helper index
	idx := map[string]any{
		"formatVersion": 1, "game": "minecraft", "versionId": "1.2", "name": "Testpack",
		"dependencies": map[string]string{"minecraft": "1.21.1"},
		"files": []any{
			map[string]any{"path": "mods/a.jar", "hashes": map[string]string{"sha1": hex.EncodeToString(h[:])}, "env": map[string]string{"client": "required", "server": "required"}, "downloads": []string{"https://placeholder/a.jar"}, "fileSize": len(jarA)},
			map[string]any{"path": "mods/server-only.jar", "hashes": map[string]string{}, "env": map[string]string{"client": "unsupported", "server": "required"}, "downloads": []string{"https://placeholder/s.jar"}},
		},
	}
	b, _ := json.Marshal(idx)
	pack := filepath.Join(t.TempDir(), "p.mrpack")
	writeZip(t, pack, map[string]string{
		"modrinth.index.json":          strings.ReplaceAll(string(b), "https://placeholder", strings.Replace(srv.URL, "http://", "https://", 1)),
		"overrides/config/x.toml":      "a=1",
		"client-overrides/options.txt": "fov:90",
	})
	// route the fake https URL to the plain http test server
	oldT := dlClient.Transport
	dlClient.Transport = rewriteTransport{srv.URL}
	defer func() { dlClient.Transport = oldT }()

	j := &Job{}
	res, err := importModpack(j, pack, ModpackRef{}, "")
	if err != nil {
		t.Fatal(err, j.Log)
	}
	in := res.Instance
	if in.MCVersion != "1.21.1" || in.Loader != "vanilla" || in.Modpack == nil || in.Modpack.Name != "Testpack" {
		t.Fatalf("instance: %+v", in)
	}
	if b, _ := os.ReadFile(filepath.Join(in.Dir, "mods", "a.jar")); string(b) != string(jarA) {
		t.Fatal("mod not downloaded")
	}
	if fileExists(filepath.Join(in.Dir, "mods", "server-only.jar")) || res.Skipped != 1 {
		t.Fatal("server-only file must be skipped")
	}
	if !fileExists(filepath.Join(in.Dir, "config", "x.toml")) || !fileExists(filepath.Join(in.Dir, "options.txt")) {
		t.Fatal("overrides not extracted")
	}
}

type rewriteTransport struct{ base string }

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	u.Scheme = "http"
	u.Host = strings.TrimPrefix(r.base, "http://")
	nr := req.Clone(req.Context())
	nr.URL = &u
	nr.Host = u.Host
	return http.DefaultTransport.RoundTrip(nr)
}

func TestModpackRejectsPathTraversal(t *testing.T) {
	withTempConfig(t)
	for _, bad := range []string{"../evil.jar", "mods/../../evil.jar", "/abs.jar", "C:/x.jar"} {
		if _, err := safeJoin(t.TempDir(), bad); err == nil {
			t.Errorf("safeJoin accepted %q", bad)
		}
	}
	idx := `{"formatVersion":1,"game":"minecraft","name":"Evil","dependencies":{"minecraft":"1.21.1"},"files":[{"path":"../../evil.jar","hashes":{},"env":{"client":"required"},"downloads":["https://x/e.jar"]}]}`
	pack := filepath.Join(t.TempDir(), "evil.mrpack")
	writeZip(t, pack, map[string]string{"modrinth.index.json": idx})
	if _, err := importModpack(&Job{}, pack, ModpackRef{}, ""); err == nil || !strings.Contains(err.Error(), "ungültiger Pfad") {
		t.Fatalf("expected path error, got %v", err)
	}
	pack2 := filepath.Join(t.TempDir(), "evil2.mrpack")
	writeZip(t, pack2, map[string]string{
		"modrinth.index.json":      `{"formatVersion":1,"game":"minecraft","name":"Evil2","dependencies":{"minecraft":"1.21.1"},"files":[]}`,
		"overrides/../../evil.txt": "x",
	})
	if _, err := importModpack(&Job{}, pack2, ModpackRef{}, ""); err == nil {
		t.Fatal("override path traversal must fail")
	}
}

func TestCheckUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"V1.2.0","html_url":"https://github.com/x","body":"neu","assets":[{"name":"CraftKit.exe","browser_download_url":"https://x/CraftKit.exe"},{"name":"CraftKit.exe.sha256","browser_download_url":"https://x/s"}]}`))
	}))
	defer srv.Close()
	oldAPI, oldVer := githubAPI, appVersion
	defer func() { githubAPI, appVersion = oldAPI, oldVer }()
	githubAPI = srv.URL
	cases := map[string]bool{"1.1.0": true, "1.2.0": false, "1.3.0": false, "dev": false, "1.1.0-test": true}
	for cur, want := range cases {
		appVersion = cur
		cacheMu.Lock()
		cache = map[string]cacheEntry{}
		cacheMu.Unlock()
		u, err := checkUpdate()
		if err != nil {
			t.Fatal(err)
		}
		if u.Available != want || u.Latest != "1.2.0" {
			t.Errorf("current %s: available=%v latest=%s", cur, u.Available, u.Latest)
		}
	}
}
