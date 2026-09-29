package main

import (
	"fmt"
	"sort"
	"strings"
)

type Project struct {
	Source     string   `json:"source"`
	ID         string   `json:"id"`
	Slug       string   `json:"slug"`
	Name       string   `json:"name"`
	Summary    string   `json:"summary"`
	Author     string   `json:"author"`
	IconURL    string   `json:"iconUrl"`
	PageURL    string   `json:"pageUrl"`
	Downloads  int64    `json:"downloads"`
	Categories []string `json:"categories,omitempty"`
}

type ModFile struct {
	URL       string `json:"url"` // empty if the author disallows third-party downloads
	FileName  string `json:"fileName"`
	Size      int64  `json:"size"`
	Hash      *Hash  `json:"hash,omitempty"`
	ManualURL string `json:"manualUrl,omitempty"`
}

type Dep struct {
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId,omitempty"`
	Kind      string `json:"kind"` // required, optional, incompatible, embedded
}

type ModVersion struct {
	Source       string   `json:"source"`
	ID           string   `json:"id"`
	ProjectID    string   `json:"projectId"`
	Name         string   `json:"name"`
	Number       string   `json:"number"`
	Type         string   `json:"type"` // release, beta, alpha
	Date         string   `json:"date"`
	GameVersions []string `json:"gameVersions"`
	Loaders      []string `json:"loaders"`
	File         *ModFile `json:"file"`
	Deps         []Dep    `json:"deps"`
	Note         string   `json:"note,omitempty"`
}

type SearchQuery struct {
	Query     string
	Kind      string // mod, plugin
	MCVersion string
	Loaders   []string
	Offset    int
	Limit     int
}

type Provider interface {
	Name() string
	Search(q SearchQuery) ([]Project, int, error)
	Project(id string) (*Project, error)
	// Versions returns versions compatible with mc/loaders, newest first.
	Versions(projectID, kind, mc string, loaders []string) ([]ModVersion, error)
	Version(projectID, versionID string) (*ModVersion, error)
}

func providerFor(source string) (Provider, error) {
	switch source {
	case "modrinth":
		return modrinth{}, nil
	case "curseforge":
		key := strings.TrimSpace(getConfig().CurseForgeKey)
		if key == "" {
			return nil, fmt.Errorf("für CurseForge wird ein API-Key benötigt – bitte in den Einstellungen eintragen")
		}
		return curseforge{key: key}, nil
	}
	return nil, fmt.Errorf("unbekannte Quelle %q", source)
}

var sourceNames = map[string]string{"modrinth": "Modrinth", "curseforge": "CurseForge"}

// pickBest chooses the newest release, falling back to beta, then alpha.
func pickBest(vs []ModVersion) *ModVersion {
	if len(vs) == 0 {
		return nil
	}
	sorted := append([]ModVersion(nil), vs...)
	sort.SliceStable(sorted, func(i, k int) bool { return sorted[i].Date > sorted[k].Date })
	for _, t := range []string{"release", "beta", "alpha"} {
		for i := range sorted {
			if sorted[i].Type == t {
				return &sorted[i]
			}
		}
	}
	return &sorted[0]
}

func containsAny(have, want []string) bool {
	for _, h := range have {
		for _, w := range want {
			if strings.EqualFold(h, w) {
				return true
			}
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
