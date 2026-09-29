package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeProvider struct {
	projects map[string]*Project
	versions map[string][]ModVersion // by project
}

func (f *fakeProvider) Name() string                                 { return "fake" }
func (f *fakeProvider) Search(q SearchQuery) ([]Project, int, error) { return nil, 0, nil }
func (f *fakeProvider) Project(id string) (*Project, error) {
	if p, ok := f.projects[id]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("404")
}
func (f *fakeProvider) Versions(pid, kind, mc string, loaders []string) ([]ModVersion, error) {
	return f.versions[pid], nil
}
func (f *fakeProvider) Version(pid, vid string) (*ModVersion, error) {
	for _, vs := range f.versions {
		for _, v := range vs {
			if v.ID == vid {
				v := v
				return &v, nil
			}
		}
	}
	return nil, fmt.Errorf("404")
}

func ver(pid, id, date, mc string, deps ...Dep) ModVersion {
	return ModVersion{Source: "modrinth", ID: id, ProjectID: pid, Number: id, Type: "release", Date: date,
		GameVersions: []string{mc}, Loaders: []string{"fabric"},
		File: &ModFile{URL: "https://x/" + id + ".jar", FileName: id + ".jar"}, Deps: deps}
}

func newFake() *fakeProvider {
	f := &fakeProvider{projects: map[string]*Project{}, versions: map[string][]ModVersion{}}
	for _, n := range []string{"A", "B", "C", "D", "E", "F"} {
		f.projects[n] = &Project{Source: "modrinth", ID: n, Slug: strings.ToLower(n) + "-mod", Name: "Mod " + n}
	}
	f.versions["A"] = []ModVersion{ver("A", "a1", "2024-01-01", "1.20.1",
		Dep{ProjectID: "B", Kind: "required"},
		Dep{ProjectID: "D", Kind: "optional"},
		Dep{ProjectID: "E", Kind: "incompatible"})}
	// B: newest version is for another MC version – must pick b1
	f.versions["B"] = []ModVersion{
		ver("B", "b2", "2024-06-01", "1.21", Dep{ProjectID: "C", Kind: "required"}),
		ver("B", "b1", "2024-02-01", "1.20.1", Dep{ProjectID: "C", Kind: "required"}),
	}
	f.versions["C"] = []ModVersion{ver("C", "c2", "2024-05-01", "1.20.1")}
	f.versions["D"] = []ModVersion{ver("D", "d1", "2024-01-01", "1.20.1")}
	f.versions["E"] = []ModVersion{ver("E", "e1", "2024-01-01", "1.20.1")}
	// F requires a project with no compatible version
	f.versions["F"] = []ModVersion{ver("F", "f1", "2024-01-01", "1.20.1", Dep{ProjectID: "G", Kind: "required"})}
	f.projects["G"] = &Project{Source: "modrinth", ID: "G", Name: "Mod G"}
	f.versions["G"] = []ModVersion{ver("G", "g1", "2024-01-01", "1.19.2")}
	return f
}

func testTarget() *Target {
	return &Target{Type: "instance", ID: "t", Name: "Test", Kind: "mod", MCVersion: "1.20.1", Loaders: []string{"fabric"},
		Items: map[string]*InstalledItem{
			"modrinth:C": {Key: "modrinth:C", Source: "modrinth", ProjectID: "C", Name: "Mod C", VersionID: "c1", VersionNumber: "c1", VersionDate: "2024-01-01"},
			"modrinth:E": {Key: "modrinth:E", Source: "modrinth", ProjectID: "E", Name: "Mod E", VersionID: "e1", Explicit: true},
		}}
}

func TestResolveDependencies(t *testing.T) {
	f := newFake()
	r := &resolver{provider: func(string) (Provider, error) { return f, nil }, projects: map[string]*Project{}}
	plan := r.resolve(testTarget(), []PlanRequest{{Source: "modrinth", ProjectID: "A"}})

	got := map[string]*PlanItem{}
	for _, it := range plan.Items {
		got[it.ProjectID] = it
	}
	if a := got["A"]; a == nil || a.Action != "install" || !a.Explicit {
		t.Fatalf("A should be installed explicitly: %+v", a)
	}
	b := got["B"]
	if b == nil || b.Action != "install" || b.Explicit || b.Version.ID != "b1" {
		t.Fatalf("B should be auto-installed with compatible version b1: %+v", b)
	}
	if len(b.RequiredBy) != 1 || b.RequiredBy[0] != "Mod A" {
		t.Fatalf("B requiredBy wrong: %v", b.RequiredBy)
	}
	if c := got["C"]; c == nil || c.Action != "keep" {
		t.Fatalf("C is installed, dependency should be kept: %+v", c)
	}
	if len(plan.Optional) != 1 || plan.Optional[0].ProjectID != "D" {
		t.Fatalf("D should be suggested as optional: %+v", plan.Optional)
	}
	if len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0], "Mod E") {
		t.Fatalf("conflict with E expected: %v", plan.Conflicts)
	}
	if len(plan.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", plan.Errors)
	}
	if !contains(got["A"].Dependencies, "modrinth:B") || !contains(b.Dependencies, "modrinth:C") {
		t.Fatalf("dependency keys not recorded")
	}
	if plan.Items[0].ProjectID != "A" {
		t.Fatalf("explicit items should come first")
	}
}

func TestResolveMissingDependency(t *testing.T) {
	f := newFake()
	r := &resolver{provider: func(string) (Provider, error) { return f, nil }, projects: map[string]*Project{}}
	plan := r.resolve(testTarget(), []PlanRequest{{Source: "modrinth", ProjectID: "F"}})
	if len(plan.Errors) != 1 || !strings.Contains(plan.Errors[0], "Mod G") || !strings.Contains(plan.Errors[0], "Mod F") {
		t.Fatalf("expected missing-dependency error, got %v", plan.Errors)
	}
}

func TestUpdateDetection(t *testing.T) {
	f := newFake()
	r := &resolver{provider: func(string) (Provider, error) { return f, nil }, projects: map[string]*Project{}}
	tg := testTarget()
	plan := r.resolveUpdateAll(tg, []PlanRequest{{Source: "modrinth", ProjectID: "C"}})
	if len(plan.Items) != 1 || plan.Items[0].Action != "update" || plan.Items[0].Version.ID != "c2" {
		t.Fatalf("C should be updated to c2: %+v", plan.Items[0])
	}
	if plan.Items[0].Explicit {
		t.Fatalf("update-all must keep dependency status")
	}
}

func TestReverseConflict(t *testing.T) {
	f := newFake()
	r := &resolver{provider: func(string) (Provider, error) { return f, nil }, projects: map[string]*Project{}}
	tg := testTarget()
	tg.Items["modrinth:E"].Incompatible = []string{"modrinth:D"}
	plan := r.resolve(tg, []PlanRequest{{Source: "modrinth", ProjectID: "D"}})
	if len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0], "Mod E") {
		t.Fatalf("installed E declares D incompatible, expected conflict: %v", plan.Conflicts)
	}
}

func TestOrphansAndDependents(t *testing.T) {
	tg := &Target{Items: map[string]*InstalledItem{
		"m:A": {Key: "m:A", Name: "A", Explicit: true, Dependencies: []string{"m:B"}},
		"m:B": {Key: "m:B", Name: "B", Dependencies: []string{"m:C"}},
		"m:C": {Key: "m:C", Name: "C"},
		"m:X": {Key: "m:X", Name: "X", Explicit: true, Dependencies: []string{"m:C"}},
	}}
	if d := dependents(tg, "m:B"); len(d) != 1 || d[0] != "A" {
		t.Fatalf("dependents of B: %v", d)
	}
	o := orphansAfterRemoval(tg, "m:A")
	if len(o) != 1 || o[0].Name != "B" {
		t.Fatalf("only B becomes orphan (C still needed by X): %v", o)
	}
}

func TestVersionMapping(t *testing.T) {
	cases := map[string]string{
		"20.4.237": "1.20.4", "21.0.10-beta": "1.21", "21.1.72": "1.21.1", "20.2.3-beta": "1.20.2",
		"26.1.0.5-beta": "26.1", "26.1.1.3": "26.1.1",
	}
	for in, want := range cases {
		if got := neoforgeMC(in); got != want {
			t.Errorf("neoforgeMC(%s)=%s want %s", in, got, want)
		}
	}
	if forgeMC("1.7.10-10.13.4.1614-1.7.10") != "1.7.10" || forgeShort("1.20.1-47.2.0") != "47.2.0" {
		t.Error("forge parsing")
	}
	if compareVersions("47.2.10", "47.2.9") <= 0 || compareVersions("21.1.0", "21.1.0-beta") <= 0 {
		t.Error("compareVersions")
	}
}

func TestSafeFileName(t *testing.T) {
	if safeFileName(`..\..\evil.jar`) != "evil.jar" || safeFileName("a:b?.jar") != "a_b_.jar" {
		t.Error("safeFileName")
	}
}

func TestIncompatibleInstalledGetsReplaced(t *testing.T) {
	f := newFake()
	r := &resolver{provider: func(string) (Provider, error) { return f, nil }, projects: map[string]*Project{}}
	tg := testTarget()
	// B installed in a version for 1.21 with a *newer* date than the 1.20.1 build
	tg.Items["modrinth:B"] = &InstalledItem{Key: "modrinth:B", Source: "modrinth", ProjectID: "B", Name: "Mod B", VersionID: "b2", VersionDate: "2024-06-01", Explicit: true}
	plan := r.resolveUpdateAll(tg, []PlanRequest{{Source: "modrinth", ProjectID: "B"}})
	var b *PlanItem
	for _, it := range plan.Items {
		if it.ProjectID == "B" {
			b = it
		}
	}
	if b == nil || b.Action != "update" || b.Version.ID != "b1" {
		t.Fatalf("B (wrong MC version) must be replaced by b1: %+v", b)
	}
}

func TestEnableDisable(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.jar"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "foreign.jar"), []byte("y"), 0o644)
	saved := 0
	tg := &Target{Dir: dir, Items: map[string]*InstalledItem{
		"m:A": {Key: "m:A", Name: "A", FileName: "a.jar"},
	}, save: func() error { saved++; return nil }}
	if err := setEnabled(tg, "m:A", "", false); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(dir, "a.jar.disabled")) || !tg.Items["m:A"].Disabled || saved != 1 {
		t.Fatal("disable managed")
	}
	if err := setEnabled(tg, "m:A", "", true); err != nil || !fileExists(filepath.Join(dir, "a.jar")) {
		t.Fatal("enable managed", err)
	}
	if err := setEnabled(tg, "", "foreign.jar", false); err != nil || !fileExists(filepath.Join(dir, "foreign.jar.disabled")) {
		t.Fatal("disable foreign", err)
	}
	if err := setEnabled(tg, "", "foreign.jar.disabled", true); err != nil || !fileExists(filepath.Join(dir, "foreign.jar")) {
		t.Fatal("enable foreign", err)
	}
	if err := setEnabled(tg, "", `..\x.jar`, true); err == nil {
		t.Fatal("path traversal must fail")
	}
	// disabled jars do not provide dependencies
	makeJar(t, filepath.Join(dir, "lib.jar.disabled"), map[string]string{"fabric.mod.json": `{"id":"lib","version":"1"}`})
	makeJar(t, filepath.Join(dir, "user.jar"), map[string]string{"fabric.mod.json": `{"id":"user","version":"1","depends":{"lib":"*"}}`})
	if m := missingDeps(scanFolder(dir), "mod"); len(m) != 1 || m[0].ModID != "lib" {
		t.Fatalf("disabled dependency should count as missing: %+v", m)
	}
}

func init() { inUnitTest = true }
