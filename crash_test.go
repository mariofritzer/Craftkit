package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCrashFabricDependencies(t *testing.T) {
	withTempConfig(t)
	in := makeTestInstance(t)
	makeJar(t, filepath.Join(in.Dir, "mods", "modmenu.jar"), map[string]string{"fabric.mod.json": `{"id":"modmenu","name":"Mod Menu","version":"11.0.2"}`})
	makeJar(t, filepath.Join(in.Dir, "mods", "sodium.jar"), map[string]string{"fabric.mod.json": `{"id":"sodium","name":"Sodium","version":"0.6.0"}`})
	os.MkdirAll(filepath.Join(in.Dir, "logs"), 0o755)
	os.WriteFile(filepath.Join(in.Dir, "logs", "latest.log"), []byte(`[12:00:01] [main/INFO]: Loading Minecraft 1.21.1 with Fabric Loader 0.15.11
[12:00:02] [main/ERROR]: Incompatible mods found!
net.fabricmc.loader.impl.FormattedException: Some of your mods are incompatible with the game or each other!
More details:
	 - Mod 'Mod Menu' (modmenu) 11.0.2 requires any version of fabric-api, which is missing!
	 - Mod 'Sodium' (sodium) 0.6.0 requires version 0.16.0 or later of fabricloader, but only the wrong version is present: 0.15.11!
	 - Mod 'Sodium' (sodium) 0.6.0 is incompatible with any version of optifabric, but a matching version is present: 1.14.0!
`), 0o644)
	suggestHook = func(ms []*MissingDep) {} // no network in tests
	defer func() { suggestHook = nil }()
	rep := analyzeCrash(in)
	if !rep.Found || len(rep.Findings) < 3 {
		t.Fatalf("findings: %+v", rep.Findings)
	}
	if rep.Findings[0].Kind != "missing" || rep.Findings[0].Missing[0].ModID != "fabric-api" || rep.Findings[0].Missing[0].NeededBy[0] != "Mod Menu" {
		t.Fatalf("missing: %+v", rep.Findings[0])
	}
	var loaderMsg, breaks bool
	for _, f := range rep.Findings {
		if f.Kind == "incompatible" && strings.Contains(f.Title, "Fabric-Loader-Version") {
			loaderMsg = true
		}
		if f.Kind == "incompatible" && strings.Contains(f.Title, "verträgt sich nicht") {
			breaks = true
			if f.Mods[0].File != "sodium.jar" {
				t.Errorf("sodium jar not resolved: %+v", f.Mods[0])
			}
		}
	}
	if !loaderMsg || !breaks {
		t.Fatalf("findings: %+v", rep.Findings)
	}
}

func TestCrashForgeReport(t *testing.T) {
	withTempConfig(t)
	in := makeTestInstance(t)
	makeJar(t, filepath.Join(in.Dir, "mods", "create-1.20.1.jar"), map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"create\"\nversion=\"0.5.1\"\ndisplayName=\"Create\"\n"})
	os.MkdirAll(filepath.Join(in.Dir, "crash-reports"), 0o755)
	os.WriteFile(filepath.Join(in.Dir, "crash-reports", "crash-2026-09-29_12.00.00-client.txt"), []byte(`---- Minecraft Crash Report ----
Description: Unexpected error

java.lang.OutOfMemoryError: Java heap space
	at com.simibubi.create.foundation.render.SuperByteBuffer.<init>(SuperByteBuffer.java:42) ~[create-1.20.1.jar%23123!/:0.5.1] {re:classloading}

-- Head --
Thread: Render thread
Suspected Mods: 
	Create (create), Version: 0.5.1
		Issue tracker URL: https://github.com/Creators-of-Create/Create/issues
Stacktrace:
	at x
`), 0o644)
	rep := analyzeCrash(in)
	if !rep.Found || rep.Description != "Unexpected error" {
		t.Fatalf("report: %+v", rep)
	}
	kinds := map[string]CrashFinding{}
	for _, f := range rep.Findings {
		kinds[f.Kind] = f
	}
	if _, ok := kinds["memory"]; !ok {
		t.Error("memory finding missing")
	}
	s, ok := kinds["suspect"]
	if !ok || len(s.Mods) != 1 || s.Mods[0].Name != "Create" || s.Mods[0].File != "create-1.20.1.jar" {
		t.Errorf("suspect: %+v", s)
	}
}

func TestNoCrash(t *testing.T) {
	withTempConfig(t)
	in := makeTestInstance(t)
	os.MkdirAll(filepath.Join(in.Dir, "logs"), 0o755)
	os.WriteFile(filepath.Join(in.Dir, "logs", "latest.log"), []byte("[main/INFO]: Stopping!\n"), 0o644)
	if rep := analyzeCrash(in); rep.Found {
		t.Fatalf("clean log reported as crash: %+v", rep)
	}
	_ = time.Now
}
