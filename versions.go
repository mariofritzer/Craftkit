package main

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	mojangManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	fabricMeta        = "https://meta.fabricmc.net/v2"
	quiltMeta         = "https://meta.quiltmc.org/v3"
	forgeMaven        = "https://maven.minecraftforge.net/net/minecraftforge/forge"
	forgePromotions   = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	neoforgeMaven     = "https://maven.neoforged.net/releases/net/neoforged/neoforge"
)

var loaderNames = map[string]string{
	"vanilla":  "Vanilla",
	"forge":    "Forge",
	"neoforge": "NeoForge",
	"fabric":   "Fabric",
	"quilt":    "Quilt",
}

type MCVersion struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // release, snapshot, old_beta, old_alpha
	Release string `json:"releaseTime"`
}

type LoaderVersion struct {
	Version     string `json:"version"`
	Stable      bool   `json:"stable"`
	Recommended bool   `json:"recommended"`
}

type mojangManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		URL         string `json:"url"`
		ReleaseTime string `json:"releaseTime"`
	} `json:"versions"`
}

func fetchMojangVersions() ([]MCVersion, error) {
	var m mojangManifest
	if err := getJSON(mojangManifestURL, nil, 30*time.Minute, &m); err != nil {
		return nil, err
	}
	out := make([]MCVersion, 0, len(m.Versions))
	for _, v := range m.Versions {
		out = append(out, MCVersion{ID: v.ID, Type: v.Type, Release: v.ReleaseTime})
	}
	return out, nil
}

// supportedGameVersions returns the set of MC versions the loader supports (nil = all).
func supportedGameVersions(loader string) (map[string]bool, error) {
	set := map[string]bool{}
	switch loader {
	case "vanilla":
		return nil, nil
	case "fabric", "quilt":
		base := fabricMeta
		if loader == "quilt" {
			base = quiltMeta
		}
		var list []struct {
			Version string `json:"version"`
		}
		if err := getJSON(base+"/versions/game", nil, 30*time.Minute, &list); err != nil {
			return nil, err
		}
		for _, v := range list {
			set[v.Version] = true
		}
	case "forge":
		vs, err := forgeAllVersions()
		if err != nil {
			return nil, err
		}
		for _, v := range vs {
			set[forgeMC(v)] = true
		}
	case "neoforge":
		vs, err := mavenVersions(neoforgeMaven + "/maven-metadata.xml")
		if err != nil {
			return nil, err
		}
		for _, v := range vs {
			if mc := neoforgeMC(v); mc != "" {
				set[mc] = true
			}
		}
	default:
		return nil, fmt.Errorf("unbekannter Loader %q", loader)
	}
	return set, nil
}

// gameVersionsFor lists MC versions (newest first) usable with the loader.
func gameVersionsFor(loader string, snapshots bool) ([]MCVersion, error) {
	all, err := fetchMojangVersions()
	if err != nil {
		return nil, err
	}
	sup, err := supportedGameVersions(loader)
	if err != nil {
		return nil, err
	}
	var out []MCVersion
	for _, v := range all {
		if !snapshots && v.Type != "release" {
			continue
		}
		if sup != nil && !sup[v.ID] {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func loaderVersionsFor(loader, mc string) ([]LoaderVersion, error) {
	var out []LoaderVersion
	switch loader {
	case "vanilla":
		return nil, nil
	case "fabric":
		var list []struct {
			Loader struct {
				Version string `json:"version"`
				Stable  bool   `json:"stable"`
			} `json:"loader"`
		}
		if err := getJSON(fabricMeta+"/versions/loader/"+url.PathEscape(mc), nil, 10*time.Minute, &list); err != nil {
			return nil, err
		}
		for _, l := range list {
			out = append(out, LoaderVersion{Version: l.Loader.Version, Stable: l.Loader.Stable})
		}
	case "quilt":
		var list []struct {
			Loader struct {
				Version string `json:"version"`
			} `json:"loader"`
		}
		if err := getJSON(quiltMeta+"/versions/loader/"+url.PathEscape(mc), nil, 10*time.Minute, &list); err != nil {
			return nil, err
		}
		for _, l := range list {
			v := l.Loader.Version
			out = append(out, LoaderVersion{Version: v, Stable: !strings.Contains(v, "-")})
		}
	case "forge":
		all, err := forgeAllVersions()
		if err != nil {
			return nil, err
		}
		var promos struct {
			Promos map[string]string `json:"promos"`
		}
		_ = getJSON(forgePromotions, nil, 30*time.Minute, &promos)
		rec := promos.Promos[mc+"-recommended"]
		latest := promos.Promos[mc+"-latest"]
		for _, full := range all {
			if forgeMC(full) != mc {
				continue
			}
			short := forgeShort(full)
			out = append(out, LoaderVersion{Version: full, Stable: true, Recommended: short == rec || (rec == "" && short == latest)})
		}
		sort.SliceStable(out, func(i, k int) bool {
			return compareVersions(forgeShort(out[i].Version), forgeShort(out[k].Version)) > 0
		})
	case "neoforge":
		all, err := mavenVersions(neoforgeMaven + "/maven-metadata.xml")
		if err != nil {
			return nil, err
		}
		for _, v := range all {
			if neoforgeMC(v) == mc {
				out = append(out, LoaderVersion{Version: v, Stable: !strings.Contains(v, "-")})
			}
		}
		sort.SliceStable(out, func(i, k int) bool { return compareVersions(out[i].Version, out[k].Version) > 0 })
	default:
		return nil, fmt.Errorf("unbekannter Loader %q", loader)
	}
	// mark first stable as recommended if nothing else is
	hasRec := false
	for _, v := range out {
		if v.Recommended {
			hasRec = true
		}
	}
	if !hasRec && len(out) > 0 {
		idx := 0
		for i := range out {
			if out[i].Stable {
				idx = i
				break
			}
		}
		out[idx].Recommended = true
	}
	return out, nil
}

type mavenMetadata struct {
	Versioning struct {
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

func mavenVersions(metaURL string) ([]string, error) {
	b, err := getBytes(metaURL, map[string]string{"Accept": "application/xml"}, 30*time.Minute)
	if err != nil {
		return nil, err
	}
	var m mavenMetadata
	if err := xml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("maven-metadata nicht lesbar: %w", err)
	}
	return m.Versioning.Versions.Version, nil
}

func forgeAllVersions() ([]string, error) {
	return mavenVersions(forgeMaven + "/maven-metadata.xml")
}

// forgeMC: "1.20.1-47.2.0" -> "1.20.1"; "1.7.10-10.13.4.1614-1.7.10" -> "1.7.10"
func forgeMC(full string) string {
	if i := strings.Index(full, "-"); i > 0 {
		mc := full[:i]
		// Forge used "1.7.10_pre4" for prerelease MC
		return strings.ReplaceAll(mc, "_", "-")
	}
	return full
}

// forgeShort: "1.20.1-47.2.0" -> "47.2.0"
func forgeShort(full string) string {
	parts := strings.Split(full, "-")
	if len(parts) >= 2 {
		return parts[1]
	}
	return full
}

// neoforgeMC maps a NeoForge version to its Minecraft version.
//
//	20.4.237      -> 1.20.4
//	21.0.10-beta  -> 1.21
//	21.1.72       -> 1.21.1
//	26.1.0.5-beta -> 26.1   (year based versioning since 2026)
//	26.1.1.3      -> 26.1.1
func neoforgeMC(v string) string {
	base := v
	if i := strings.IndexAny(base, "-+"); i >= 0 {
		base = base[:i]
	}
	p := strings.Split(base, ".")
	if len(p) < 2 {
		return ""
	}
	major, err := strconv.Atoi(p[0])
	if err != nil {
		return ""
	}
	if major >= 25 {
		if len(p) >= 4 {
			if p[2] == "0" {
				return p[0] + "." + p[1]
			}
			return p[0] + "." + p[1] + "." + p[2]
		}
		return p[0] + "." + p[1]
	}
	if major < 20 {
		return ""
	}
	if p[1] == "0" {
		return "1." + p[0]
	}
	return "1." + p[0] + "." + p[1]
}

// compareVersions compares dotted numeric versions ("47.2.10" > "47.2.9").
// Non-numeric suffixes rank lower than the plain version.
func compareVersions(a, b string) int {
	split := func(s string) ([]int, string) {
		suffix := ""
		if i := strings.IndexAny(s, "-+"); i >= 0 {
			suffix = s[i:]
			s = s[:i]
		}
		var nums []int
		for _, x := range strings.Split(s, ".") {
			n, _ := strconv.Atoi(x)
			nums = append(nums, n)
		}
		return nums, suffix
	}
	an, as := split(a)
	bn, bs := split(b)
	for i := 0; i < len(an) || i < len(bn); i++ {
		var x, y int
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	if as == bs {
		return 0
	}
	if as == "" {
		return 1
	}
	if bs == "" {
		return -1
	}
	return strings.Compare(as, bs)
}
