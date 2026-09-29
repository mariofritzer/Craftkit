package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportImportRoundtrip(t *testing.T) {
	withTempConfig(t)
	in := makeTestInstance(t)
	in.Loader, in.LoaderVersion = "vanilla", "" // keeps the re-import offline
	mods := filepath.Join(in.Dir, "mods")
	jar := []byte("sodium-jar-bytes")
	os.WriteFile(filepath.Join(mods, "sodium.jar"), jar, 0o644)
	os.WriteFile(filepath.Join(mods, "custom.jar"), []byte("my own mod"), 0o644)
	os.WriteFile(filepath.Join(mods, "off.jar.disabled"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(in.Dir, "config"), 0o755)
	os.WriteFile(filepath.Join(in.Dir, "config", "sodium.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(in.Dir, "options.txt"), []byte("fov:90"), 0o644)
	s1, _, _, _ := fileHashes(filepath.Join(mods, "sodium.jar"))
	in.Items["modrinth:AANobbMI"] = &InstalledItem{Key: "modrinth:AANobbMI", Source: "modrinth", ProjectID: "AANobbMI", Name: "Sodium", VersionID: "v1", FileName: "sodium.jar", Explicit: true}
	in.save()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/versions"):
			w.Write([]byte(`[{"id":"v1","project_id":"AANobbMI","files":[{"url":"https://cdn.modrinth.com/data/AANobbMI/versions/v1/sodium.jar","filename":"sodium.jar","primary":true,"hashes":{"sha1":"` + s1 + `"}}]}]`))
		case strings.HasPrefix(r.URL.Path, "/projects"):
			w.Write([]byte(`[{"id":"AANobbMI","client_side":"required","server_side":"unsupported"}]`))
		case strings.HasSuffix(r.URL.Path, "sodium.jar"):
			w.Write(jar)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	oldAPI := modrinthAPI
	modrinthAPI = api.URL
	defer func() { modrinthAPI = oldAPI }()

	res, err := exportInstance("test", ExportOptions{Name: "Unser Pack", Version: "1.0", Config: true, Options: false, Servers: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Linked != 1 || len(res.Packed) != 1 || res.Packed[0] != "custom.jar" {
		t.Fatalf("linked=%d packed=%v", res.Linked, res.Packed)
	}
	zr, err := zip.OpenReader(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var idx map[string]any
	for _, f := range zr.File {
		names[f.Name] = true
		if f.Name == "modrinth.index.json" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			json.Unmarshal(b, &idx)
		}
	}
	zr.Close()
	if !names["overrides/mods/custom.jar"] || !names["overrides/config/sodium.json"] || names["client-overrides/options.txt"] || names["overrides/mods/sodium.jar"] {
		t.Fatalf("zip contents: %v", names)
	}
	files := idx["files"].([]any)
	env := files[0].(map[string]any)["env"].(map[string]any)
	if len(files) != 1 || env["server"] != "unsupported" {
		t.Fatalf("index files: %v", files)
	}

	// importing the export recreates the instance
	oldT := dlClient.Transport
	dlClient.Transport = rewriteTransport{api.URL}
	defer func() { dlClient.Transport = oldT }()
	imp, err := importModpack(&Job{}, res.Path, ModpackRef{}, "Freund")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"mods/sodium.jar", "mods/custom.jar", "config/sodium.json"} {
		if !fileExists(filepath.Join(imp.Instance.Dir, f)) {
			t.Errorf("missing after import: %s", f)
		}
	}
	if fileExists(filepath.Join(imp.Instance.Dir, "mods", "off.jar.disabled")) {
		t.Error("disabled mod must not be shared")
	}
}
