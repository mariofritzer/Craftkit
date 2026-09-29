package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func makeJar(t *testing.T, path string, files map[string]string) {
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

func jarBytes(files map[string]string) string {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	return buf.String()
}

func TestScanFolderAndMissingDeps(t *testing.T) {
	dir := t.TempDir()
	// Fabric API bundles its modules as jar-in-jar
	makeJar(t, filepath.Join(dir, "fabric-api.jar"), map[string]string{
		"fabric.mod.json":        `{"id":"fabric-api","name":"Fabric API","version":"0.105.0","jars":[{"file":"META-INF/jars/base.jar"}]}`,
		"META-INF/jars/base.jar": jarBytes(map[string]string{"fabric.mod.json": `{"id":"fabric-api-base","version":"1"}`}),
	})
	makeJar(t, filepath.Join(dir, "sodium.jar"), map[string]string{
		"fabric.mod.json": "{\n // comment\n \"id\":\"sodium\",\"name\":\"Sodium\",\"version\":\"0.6\",\"depends\":{\"fabricloader\":\">=0.15\",\"minecraft\":\"1.21.1\",\"fabric-api-base\":\"*\"}}",
	})
	makeJar(t, filepath.Join(dir, "iris.jar"), map[string]string{
		"fabric.mod.json": `{"id":"iris","name":"Iris","version":"1.8","depends":{"sodium":"*","cloth-config2":"*"}}`,
	})
	makeJar(t, filepath.Join(dir, "create.jar"), map[string]string{
		"META-INF/mods.toml": `modLoader="javafml"
loaderVersion="[47,)"
[[mods]]
modId="create"
version="${file.jarVersion}"
displayName="Create"
description='''
A mod
'''
[[dependencies.create]]
    modId="forge"
    mandatory=true
[[dependencies.create]]
    modId="flywheel"
    mandatory=true
    side="CLIENT"
[[dependencies.create]]
    modId="jei"
    mandatory=false
`,
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nImplementation-Version: 0.5.1\n",
	})
	jars := scanFolder(dir)
	if len(jars) != 4 {
		t.Fatalf("expected 4 jars, got %d", len(jars))
	}
	by := map[string]*JarInfo{}
	for _, j := range jars {
		by[j.ModID] = j
	}
	if by["create"] == nil || by["create"].Version != "0.5.1" || by["create"].Loader != "forge" || by["create"].Name != "Create" {
		t.Fatalf("mods.toml parse: %+v", by["create"])
	}
	if !contains(by["create"].Depends, "flywheel") || contains(by["create"].Depends, "jei") {
		t.Fatalf("create deps: %v", by["create"].Depends)
	}
	if by["sodium"] == nil || by["sodium"].Name != "Sodium" {
		t.Fatalf("fabric.mod.json with comment: %+v", by["sodium"])
	}
	miss := missingDeps(jars, "mod")
	ids := []string{}
	for _, m := range miss {
		ids = append(ids, m.ModID)
	}
	// fabric-api-base is provided via jar-in-jar; cloth-config2 and flywheel are really missing
	if len(miss) != 2 || !contains(ids, "cloth-config2") || !contains(ids, "flywheel") {
		t.Fatalf("missing deps: %v", ids)
	}
}

func TestPluginYml(t *testing.T) {
	name, ver, _, deps := parsePluginYml("name: Towny\nversion: '0.100'\ndepend: [Vault, 'PlaceholderAPI']\nsoftdepend:\n  - Essentials\n", false)
	if name != "Towny" || ver != "0.100" || len(deps) != 2 || deps[1] != "PlaceholderAPI" {
		t.Fatalf("inline: %s %s %v", name, ver, deps)
	}
	_, _, _, deps = parsePluginYml("name: X\ndepend:\n  - Vault\n  - WorldEdit\nloadbefore:\n  - Foo\n", false)
	if len(deps) != 2 || deps[1] != "WorldEdit" {
		t.Fatalf("list: %v", deps)
	}
	_, _, _, deps = parsePluginYml("name: P\nversion: 1\ndependencies:\n  server:\n    LuckPerms:\n      load: BEFORE\n      required: true\n    Vault:\n      required: false\n", true)
	if len(deps) != 1 || deps[0] != "LuckPerms" {
		t.Fatalf("paper: %v", deps)
	}
}

func TestDetectVersion(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][3]string{
		"fabric-loader-0.16.5-1.21.1":     {"fabric", "0.16.5", "1.21.1"},
		"quilt-loader-0.27.0-beta.1-1.21": {"quilt", "0.27.0-beta.1", "1.21"},
		"1.20.1-forge-47.2.0":             {"forge", "47.2.0", "1.20.1"},
		"1.7.10-Forge10.13.4.1614-1.7.10": {"forge", "10.13.4.1614", "1.7.10"},
		"neoforge-21.1.72":                {"neoforge", "21.1.72", "1.21.1"},
		"1.20.1-OptiFine_HD_U_I6":         {"optifine", "", "1.20.1"},
		"1.21.1":                          {"vanilla", "", "1.21.1"},
	}
	for id, want := range cases {
		v := detectVersion(dir, id)
		if v.Loader != want[0] || v.LoaderVersion != want[1] || v.MCVersion != want[2] {
			t.Errorf("%s -> %s/%s/%s want %v", id, v.Loader, v.LoaderVersion, v.MCVersion, want)
		}
	}
}

func TestServersDat(t *testing.T) {
	dir := t.TempDir()
	if added, err := addServerToList(dir, "Mein Server", "play.example.at"); err != nil || !added {
		t.Fatal(err)
	}
	if added, _ := addServerToList(dir, "Mein Server", "play.example.at"); added {
		t.Fatal("duplicate added")
	}
	if added, err := addServerToList(dir, "Zweiter", "mc.test:25570"); err != nil || !added {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "servers.dat"))
	r := bytes.NewReader(b)
	r.ReadByte()
	var n uint16
	binary.Read(r, binary.BigEndian, &n)
	v, err := nbtReadPayload(r, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	list := v.([]*nbtTag)[0].Value.(nbtList)
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(list.Items))
	}
	second := list.Items[1].([]*nbtTag)
	if second[1].Value.(string) != "mc.test:25570" {
		t.Fatalf("ip: %v", second[1].Value)
	}
}

// encodeForgeData mirrors Forge's encodeOptimized for the test.
func encodeForgeData(data []byte) string {
	var rs []rune
	rs = append(rs, rune(len(data)&0x7FFF), rune(len(data)>>15&0x7FFF))
	var buffer uint64
	bits := 0
	for _, b := range data {
		buffer |= uint64(b) << bits
		bits += 8
		for bits >= 15 {
			rs = append(rs, rune(buffer&0x7FFF))
			buffer >>= 15
			bits -= 15
		}
	}
	if bits > 0 {
		rs = append(rs, rune(buffer&0x7FFF))
	}
	return string(rs)
}

func TestServerPing(t *testing.T) {
	// forgeData.d payload: not truncated, 3 mods (one server-only)
	var p bytes.Buffer
	p.WriteByte(0)
	binary.Write(&p, binary.BigEndian, uint16(3))
	writeVarInt(&p, 1<<1) // 1 channel, has version
	writeString(&p, "create")
	writeString(&p, "0.5.1")
	writeString(&p, "create:main")
	writeString(&p, "1")
	p.WriteByte(1)
	writeVarInt(&p, 0)
	writeString(&p, "jei")
	writeString(&p, "15.2")
	writeVarInt(&p, 1) // server only
	writeString(&p, "servercore")
	status := map[string]any{
		"version":     map[string]any{"name": "1.20.1", "protocol": 763},
		"players":     map[string]any{"online": 3, "max": 20},
		"description": map[string]any{"text": "§aWillkommen", "extra": []any{map[string]any{"text": " im Test"}}},
		"forgeData":   map[string]any{"d": encodeForgeData(p.Bytes()), "mods": []any{}},
	}
	js, _ := json.Marshal(status)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no network")
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 512)
		c.Read(buf)
		var pl bytes.Buffer
		writeVarInt(&pl, 0)
		writeString(&pl, string(js))
		var out bytes.Buffer
		writePacket(&out, pl.Bytes())
		c.Write(out.Bytes())
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	info, err := pingServer("127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	if info.MCVersion != "1.20.1" || info.Loader != "forge" || info.PlayersOnline != 3 || info.MOTD != "Willkommen im Test" {
		t.Fatalf("info: %+v", info)
	}
	if len(info.Mods) != 2 || info.Mods[0].ModID != "create" || info.Mods[1].ModID != "jei" {
		t.Fatalf("mods: %+v", info.Mods)
	}
}

func TestStatusVersionGuess(t *testing.T) {
	cases := []struct {
		js    string
		mc    string
		multi bool
		soft  string
	}{
		{`{"version":{"name":"Paper 1.21.1","protocol":767}}`, "1.21.1", false, "Paper"},
		{`{"version":{"name":"Velocity 3.3.0-SNAPSHOT","protocol":767}}`, "1.21.1", false, "Velocity"},
		{`{"version":{"name":"1.8.x-1.21.x","protocol":47}}`, "1.8.9", true, ""},
		{`{"version":{"name":"1.21","protocol":767}}`, "1.21", false, ""},
	}
	for _, c := range cases {
		info, err := parseStatusJSON([]byte(c.js))
		if err != nil {
			t.Fatal(err)
		}
		if info.MCVersion != c.mc || info.MultiVersion != c.multi || !strings.EqualFold(info.Software, c.soft) {
			t.Errorf("%s -> mc=%s multi=%v soft=%q", c.js, info.MCVersion, info.MultiVersion, info.Software)
		}
	}
}
