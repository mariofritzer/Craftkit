package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeTestInstance(t *testing.T) *Instance {
	t.Helper()
	in := &Instance{ID: "test", Name: "Test", MCVersion: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.5",
		Items: map[string]*InstalledItem{}, Dir: instanceDir("test"), Created: time.Now().Format(time.RFC3339)}
	os.MkdirAll(filepath.Join(in.Dir, "mods"), 0o755)
	if err := in.save(); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestUndoInstallUpdateRemove(t *testing.T) {
	withTempConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("content of " + r.URL.Path))
	}))
	defer srv.Close()
	oldT := dlClient.Transport
	dlClient.Transport = rewriteTransport{srv.URL}
	defer func() { dlClient.Transport = oldT }()

	in := makeTestInstance(t)
	mods := filepath.Join(in.Dir, "mods")
	plan := func(action, id, file string) *Plan {
		tg, _ := loadTarget("instance", "test")
		return &Plan{Target: tg, Items: []*PlanItem{{Key: "modrinth:A", Source: "modrinth", ProjectID: "A", Name: "Mod A", Action: action, Explicit: true,
			Version: &ModVersion{ID: id, Number: id, Date: "2024-01-0" + id[1:], File: &ModFile{URL: "https://x/" + file, FileName: file}}}}}
	}
	if _, err := applyPlan(&Job{}, plan("install", "a1", "a-1.jar")); err != nil {
		t.Fatal(err)
	}
	if _, err := applyPlan(&Job{}, plan("update", "a2", "a-2.jar")); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(mods, "a-1.jar")) || !fileExists(filepath.Join(mods, "a-2.jar")) {
		t.Fatal("update did not replace the file")
	}
	tg, _ := loadTarget("instance", "test")
	if h := historyEntries(tg); len(h) != 2 {
		t.Fatalf("expected 2 history entries, got %+v", h)
	}
	if _, err := rollbackLast(tg); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(mods, "a-1.jar")) || fileExists(filepath.Join(mods, "a-2.jar")) {
		t.Fatal("rollback did not restore the old file")
	}
	tg, _ = loadTarget("instance", "test")
	if tg.Items["modrinth:A"].VersionID != "a1" {
		t.Fatalf("manifest not restored: %+v", tg.Items["modrinth:A"])
	}
	// remove + undo
	if _, err := removeItem(tg, "modrinth:A", false); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(mods, "a-1.jar")) {
		t.Fatal("remove failed")
	}
	tg, _ = loadTarget("instance", "test")
	if _, err := rollbackLast(tg); err != nil {
		t.Fatal(err)
	}
	tg, _ = loadTarget("instance", "test")
	if !fileExists(filepath.Join(mods, "a-1.jar")) || tg.Items["modrinth:A"] == nil {
		t.Fatal("undo of remove failed")
	}
	// the history lives outside the mods folder
	if fileExists(filepath.Join(mods, ".craftkit")) {
		t.Fatal("history must not be inside mods/")
	}
	// failed download must not touch existing files
	bad := plan("update", "a3", "a-3.jar")
	bad.Items[0].Version.File.Hash = &Hash{"sha1", "0000000000000000000000000000000000000000"}
	applyPlan(&Job{}, bad)
	if !fileExists(filepath.Join(mods, "a-1.jar")) || fileExists(filepath.Join(mods, "a-3.jar")) {
		t.Fatal("failed update changed files")
	}
}

func TestWorldBackupRestore(t *testing.T) {
	withTempConfig(t)
	in := makeTestInstance(t)
	w := filepath.Join(in.Dir, "saves", "Meine Welt")
	os.MkdirAll(filepath.Join(w, "region"), 0o755)
	os.WriteFile(filepath.Join(w, "level.dat"), []byte("original"), 0o644)
	os.WriteFile(filepath.Join(w, "region", "r.0.0.mca"), []byte("blocks"), 0o644)
	os.WriteFile(filepath.Join(w, "session.lock"), []byte("x"), 0o644)
	b, err := backupWorld(&Job{}, in, "Meine Welt", false)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(w, "level.dat"), []byte("broken"), 0o644)
	os.RemoveAll(filepath.Join(w, "region"))
	if err := restoreWorld(&Job{}, in, "Meine Welt", b.File); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(w, "level.dat")); string(got) != "original" {
		t.Fatalf("level.dat = %q", got)
	}
	if !fileExists(filepath.Join(w, "region", "r.0.0.mca")) {
		t.Fatal("region not restored")
	}
	ws := listWorlds(in)
	if len(ws) != 1 || len(ws[0].Backups) != 2 { // manual + automatic one before restoring
		t.Fatalf("worlds: %+v", ws[0])
	}
	for i := 0; i < 6; i++ {
		backupWorld(nil, in, "Meine Welt", true)
	}
	if n := len(listWorldBackups(in, "Meine Welt")); n != maxWorldBackups {
		t.Fatalf("expected %d backups after pruning, got %d", maxWorldBackups, n)
	}
	if _, err := backupWorld(nil, in, "../x", false); err == nil {
		t.Fatal("path traversal in world name")
	}
	if err := restoreWorld(&Job{}, in, "Meine Welt", "../../x.zip"); err == nil {
		t.Fatal("path traversal in backup name")
	}
}

func TestCheckTargetUpdates(t *testing.T) {
	withTempConfig(t)
	in := makeTestInstance(t)
	mods := filepath.Join(in.Dir, "mods")
	os.WriteFile(filepath.Join(mods, "a.jar"), []byte("A-old"), 0o644)
	os.WriteFile(filepath.Join(mods, "b.jar"), []byte("B-current"), 0o644)
	hA, _, _ := fileSHA1(filepath.Join(mods, "a.jar"))
	hB, _, _ := fileSHA1(filepath.Join(mods, "b.jar"))
	in.Items["modrinth:A"] = &InstalledItem{Key: "modrinth:A", Source: "modrinth", ProjectID: "A", Name: "Alpha", VersionID: "a1", VersionNumber: "1.0", VersionDate: "2024-01-01", FileName: "a.jar", Explicit: true}
	in.Items["modrinth:B"] = &InstalledItem{Key: "modrinth:B", Source: "modrinth", ProjectID: "B", Name: "Beta", VersionID: "b2", VersionNumber: "2.0", VersionDate: "2024-05-01", FileName: "b.jar", Explicit: true}
	in.save()
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 4096)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		w.Write([]byte(`{"` + hA + `":{"id":"a2","project_id":"A","version_number":"1.1","date_published":"2024-06-01"},
			"` + hB + `":{"id":"b2","project_id":"B","version_number":"2.0","date_published":"2024-05-01"}}`))
	}))
	defer srv.Close()
	old := modrinthAPI
	modrinthAPI = srv.URL
	defer func() { modrinthAPI = old }()
	tg, _ := loadTarget("instance", "test")
	st := checkTargetUpdates(tg)
	if st.Count != 1 || st.Items[0].Name != "Alpha" || st.Items[0].To != "1.1" {
		t.Fatalf("updates: %+v (err %s)", st, st.Error)
	}
	if !strings.Contains(gotBody, `"game_versions":["1.21.1"]`) || !strings.Contains(gotBody, `"loaders":["fabric"]`) {
		t.Fatalf("request body: %s", gotBody)
	}
}
