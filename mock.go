//go:build mock

package main

// Test mode: `go build -tags mock` answers all API calls with canned data so the
// UI and parsers can be exercised offline.

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

type mockTransport struct{}

func jar(id string) []byte { return []byte("PK-fake-jar-" + id) }

func sha(id string) string {
	h := sha512.Sum512(jar(id))
	return hex.EncodeToString(h[:])
}

var mrProjects = map[string][3]string{ // id: slug, title, description
	"AANobbMI": {"sodium", "Sodium", "Moderne Rendering-Engine für deutlich mehr FPS."},
	"P7dR8mSH": {"fabric-api", "Fabric API", "Grundlegende Schnittstellen, die fast alle Fabric-Mods brauchen."},
	"mOgUt4GM": {"modmenu", "Mod Menu", "Zeigt eine Liste aller Mods im Spiel an."},
	"9s6osm5g": {"cloth-config", "Cloth Config API", "Konfigurations-Bibliothek für viele Mods."},
	"YL57xq9U": {"iris", "Iris Shaders", "Shader-Unterstützung, arbeitet mit Sodium zusammen."},
	"Ha28R6CL": {"optifabric", "OptiFabric", "Lädt OptiFine unter Fabric."},
}

func mrVersionJSON(pid, vid, number, date string, deps string) string {
	return fmt.Sprintf(`{"id":%q,"project_id":%q,"name":%q,"version_number":%q,"version_type":"release","date_published":%q,
	"game_versions":["1.21.1"],"loaders":["fabric","quilt"],
	"files":[{"url":"https://cdn.modrinth.com/data/%s/%s.jar","filename":"%s-%s.jar","primary":true,"size":20,"hashes":{"sha512":%q}}],
	"dependencies":[%s]}`, vid, pid, number, number, date, pid, vid, mrProjects[pid][0], number, sha(vid), deps)
}

var mrVersions = map[string]string{
	"AANobbMI": mrVersionJSON("AANobbMI", "sod1", "0.6.0+mc1.21.1", "2024-09-01T00:00:00Z", `{"project_id":"P7dR8mSH","dependency_type":"required"},{"project_id":"Ha28R6CL","dependency_type":"incompatible"}`),
	"P7dR8mSH": mrVersionJSON("P7dR8mSH", "fapi1", "0.105.0+1.21.1", "2024-09-10T00:00:00Z", ``),
	"mOgUt4GM": mrVersionJSON("mOgUt4GM", "mm1", "11.0.2", "2024-08-01T00:00:00Z", `{"project_id":"P7dR8mSH","dependency_type":"required"},{"project_id":"9s6osm5g","dependency_type":"optional"}`),
	"9s6osm5g": mrVersionJSON("9s6osm5g", "cc1", "15.0.140", "2024-07-01T00:00:00Z", ``),
	"YL57xq9U": mrVersionJSON("YL57xq9U", "iris1", "1.8.0+1.21.1", "2024-09-05T00:00:00Z", `{"project_id":"AANobbMI","dependency_type":"required"}`),
	"Ha28R6CL": mrVersionJSON("Ha28R6CL", "of1", "1.14.0", "2024-01-01T00:00:00Z", ``),
}

func (mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u := r.URL.String()
	body := ""
	status := 200
	switch {
	case strings.Contains(u, "version_manifest_v2"):
		body = `{"latest":{"release":"1.21.1","snapshot":"24w40a"},"versions":[
		{"id":"24w40a","type":"snapshot","url":"","releaseTime":"2024-10-02T00:00:00Z"},
		{"id":"1.21.1","type":"release","url":"","releaseTime":"2024-08-08T00:00:00Z"},
		{"id":"1.21","type":"release","url":"","releaseTime":"2024-06-13T00:00:00Z"},
		{"id":"1.20.4","type":"release","url":"","releaseTime":"2023-12-07T00:00:00Z"},
		{"id":"1.20.1","type":"release","url":"","releaseTime":"2023-06-12T00:00:00Z"},
		{"id":"1.12.2","type":"release","url":"","releaseTime":"2017-09-18T00:00:00Z"}]}`
	case strings.Contains(u, "meta.fabricmc.net/v2/versions/game"), strings.Contains(u, "meta.quiltmc.org/v3/versions/game"):
		body = `[{"version":"24w40a","stable":false},{"version":"1.21.1","stable":true},{"version":"1.21","stable":true},{"version":"1.20.4","stable":true},{"version":"1.20.1","stable":true}]`
	case strings.Contains(u, "/profile/json"):
		parts := strings.Split(r.URL.Path, "/")
		mc, lv := parts[len(parts)-4], parts[len(parts)-3]
		name := "fabric"
		if strings.Contains(u, "quilt") {
			name = "quilt"
		}
		body = fmt.Sprintf(`{"id":"%s-loader-%s-%s","inheritsFrom":%q,"mainClass":"net.fabricmc.loader.impl.launch.knot.KnotClient","libraries":[]}`, name, lv, mc, mc)
	case strings.Contains(u, "meta.fabricmc.net/v2/versions/loader/"):
		body = `[{"loader":{"version":"0.16.5","stable":true}},{"loader":{"version":"0.16.4","stable":true}},{"loader":{"version":"0.16.6-beta.1","stable":false}}]`
	case strings.Contains(u, "meta.quiltmc.org/v3/versions/loader/"):
		body = `[{"loader":{"version":"0.27.0-beta.1"}},{"loader":{"version":"0.26.4"}}]`
	case strings.Contains(u, "minecraftforge/forge/maven-metadata.xml"):
		body = `<metadata><versioning><versions><version>1.21.1-52.0.16</version><version>1.21.1-52.0.10</version><version>1.20.1-47.3.0</version><version>1.20.1-47.2.0</version><version>1.12.2-14.23.5.2860</version></versions></versioning></metadata>`
	case strings.Contains(u, "promotions_slim"):
		body = `{"promos":{"1.20.1-recommended":"47.2.0","1.20.1-latest":"47.3.0","1.21.1-latest":"52.0.16"}}`
	case strings.Contains(u, "neoforge/maven-metadata.xml"):
		body = `<metadata><versioning><versions><version>20.4.237</version><version>21.0.167</version><version>21.1.60</version><version>21.1.72</version><version>21.2.0-beta</version></versions></versioning></metadata>`
	case strings.Contains(u, "api.modrinth.com/v2/search"):
		q := strings.ToLower(r.URL.Query().Get("query"))
		var hits []string
		for id, p := range mrProjects {
			if q != "" && !strings.Contains(strings.ToLower(p[1]), q) {
				continue
			}
			hits = append(hits, fmt.Sprintf(`{"project_id":%q,"slug":%q,"title":%q,"description":%q,"author":"jellysquid","icon_url":"","downloads":%d,"categories":["fabric"],"project_type":"mod"}`, id, p[0], p[1], p[2], 1000000+len(p[1])*731233))
		}
		body = `{"hits":[` + strings.Join(hits, ",") + fmt.Sprintf(`],"total_hits":%d}`, len(hits))
	case strings.Contains(u, "api.modrinth.com/v2/project/") && strings.Contains(u, "/version"):
		id := strings.Split(strings.Split(u, "/project/")[1], "/")[0]
		body = "[" + mrVersions[id] + "]"
	case strings.Contains(u, "api.modrinth.com/v2/project/"):
		id := strings.Split(strings.Split(u, "/project/")[1], "?")[0]
		for pid, pp := range mrProjects { // lookup by slug
			if pp[0] == id {
				id = pid
			}
		}
		p, ok := mrProjects[id]
		if !ok {
			status, body = 404, `{"error":"not_found"}`
			break
		}
		md := "# " + p[1] + "\\n\\n**Fett** und *kursiv* mit [Link](https://modrinth.com).\\n\\n- Punkt eins\\n- Punkt zwei\\n\\n<img src=x onerror=alert(1)><script>alert(2)</script><a href=\\\"javascript:alert(3)\\\">böser Link</a>\\n\\n```\\ncode\\n```"
		body = fmt.Sprintf(`{"id":%q,"slug":%q,"title":%q,"description":%q,"icon_url":"","downloads":5,"project_type":"mod","loaders":["fabric"],"body":"%s","client_side":"required","server_side":"optional","license":{"id":"MIT"},"gallery":[{"url":"https://cdn.modrinth.com/x.png","title":"Screenshot"}]}`, id, p[0], p[1], p[2], md)
	case strings.Contains(u, "api.modrinth.com/v2/version_files"):
		// the test profile contains a jar whose sha1 is that of jar("sod1")
		h := sha1.Sum(jar("sod1"))
		body = fmt.Sprintf(`{%q:%s}`, hex.EncodeToString(h[:]), mrVersions["AANobbMI"])
	case strings.Contains(u, "api.modrinth.com/v2/projects"):
		var list []string
		for id, p := range mrProjects {
			list = append(list, fmt.Sprintf(`{"id":%q,"slug":%q,"title":%q,"icon_url":""}`, id, p[0], p[1]))
		}
		body = "[" + strings.Join(list, ",") + "]"
	case strings.Contains(u, "api.modrinth.com/v2/version/"):
		vid := strings.Split(u, "/version/")[1]
		for _, v := range mrVersions {
			if strings.Contains(v, `"id":"`+vid+`"`) {
				body = v
			}
		}
	case strings.Contains(u, "cdn.modrinth.com"):
		parts := strings.Split(r.URL.Path, "/")
		vid := strings.TrimSuffix(parts[len(parts)-1], ".jar")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(jar(vid))), ContentLength: int64(len(jar(vid))), Header: http.Header{}, Request: r}, nil
	case strings.Contains(u, "api.curseforge.com"):
		status, body = 403, `{"error":"forbidden"}`
	default:
		status, body = 404, "not mocked: "+u
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

func init() {
	httpClient.Transport = mockTransport{}
	dlClient.Transport = mockTransport{}
	go fakeMinecraftServer()
}

// fakeMinecraftServer answers status pings on 127.0.0.1:25599 like a Paper 1.20.1 server.
func fakeMinecraftServer() {
	ln, err := net.Listen("tcp", "127.0.0.1:25599")
	if err != nil {
		return
	}
	status, _ := json.Marshal(map[string]any{
		"version":     map[string]any{"name": "Paper 1.20.1", "protocol": 763},
		"players":     map[string]any{"online": 7, "max": 50},
		"description": map[string]any{"text": "Wiener Survival-Server – willkommen!"},
	})
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 512)
			c.Read(buf)
			var pl bytes.Buffer
			writeVarInt(&pl, 0)
			writeString(&pl, string(status))
			var out bytes.Buffer
			writePacket(&out, pl.Bytes())
			c.Write(out.Bytes())
		}(c)
	}
}
