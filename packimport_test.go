package main

import (
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

func TestImportPackSelection(t *testing.T) {
	withTempConfig(t)
	jars := map[string][]byte{"/a.jar": []byte("jar-a"), "/b.jar": []byte("jar-b")}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := jars[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	oldAPI, oldT := modrinthAPI, dlClient.Transport
	modrinthAPI = srv.URL
	dlClient.Transport = rewriteTransport{srv.URL}
	defer func() { modrinthAPI, dlClient.Transport = oldAPI, oldT }()

	file := func(name string) any {
		h := sha1.Sum(jars["/"+name])
		return map[string]any{"path": "mods/" + name, "hashes": map[string]string{"sha1": hex.EncodeToString(h[:])},
			"downloads": []string{"https://placeholder/" + name}, "fileSize": 5}
	}
	idx := map[string]any{"formatVersion": 1, "game": "minecraft", "versionId": "1", "name": "Freunde",
		"dependencies": map[string]string{"minecraft": "1.21.1"}, "files": []any{file("a.jar"), file("b.jar")}}
	b, _ := json.Marshal(idx)
	pack := filepath.Join(t.TempDir(), "p.mrpack")
	writeZip(t, pack, map[string]string{
		"modrinth.index.json":              strings.ReplaceAll(string(b), "https://placeholder", strings.Replace(srv.URL, "http://", "https://", 1)),
		"overrides/config/x.toml":          "a=1",
		"overrides/options.txt":            "fov:90",
		"overrides/resourcepacks/Pack.zip": "zip",
		"overrides/mods/extra.jar":         "extra",
	})

	info, err := inspectPack(rememberPack(pack, "p.mrpack", ModpackRef{}))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range info.Entries {
		ids[e.ID] = true
	}
	for _, want := range []string{"f:mods/a.jar", "f:mods/b.jar", "o:mods/extra.jar", "o:resourcepacks/Pack.zip"} {
		if !ids[want] {
			t.Errorf("entry %s missing: %+v", want, info.Entries)
		}
	}
	if len(info.Groups) != 2 { // config + options
		t.Errorf("groups: %+v", info.Groups)
	}

	// new instance with only a.jar, the resource pack and the config
	j := &Job{}
	res, err := importPack(j, pack, ModpackRef{}, PackImportRequest{Name: "Auswahl", Entries: []string{"f:mods/a.jar", "o:resourcepacks/Pack.zip"}, Groups: []string{"config"}})
	if err != nil {
		t.Fatal(err, j.Log)
	}
	dir := res.Instance.Dir
	for p, want := range map[string]bool{"mods/a.jar": true, "mods/b.jar": false, "mods/extra.jar": false, "resourcepacks/Pack.zip": true, "config/x.toml": true, "options.txt": false} {
		if fileExists(filepath.Join(dir, filepath.FromSlash(p))) != want {
			t.Errorf("%s exists = %v, want %v", p, !want, want)
		}
	}

	// add b.jar and the options to the existing instance
	info2, _ := inspectPack(rememberPack(pack, "p.mrpack", ModpackRef{}))
	found := false
	for _, in := range info2.Instances {
		found = found || in.ID == res.Instance.ID
	}
	if !found {
		t.Fatalf("existing instance not offered: %+v", info2.Instances)
	}
	if _, err := importPack(j, pack, ModpackRef{}, PackImportRequest{TargetID: res.Instance.ID, Entries: []string{"f:mods/b.jar"}, Groups: []string{"options"}}); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(dir, "mods", "b.jar")) || !fileExists(filepath.Join(dir, "options.txt")) || !fileExists(filepath.Join(dir, "mods", "a.jar")) {
		t.Errorf("adding to the existing instance failed")
	}
	if n := len(listInstances()); n != 1 {
		t.Errorf("expected 1 instance, got %d", n)
	}
	os.Remove(pack)
}
