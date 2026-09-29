package main

import (
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

var modrinthAPI = "https://api.modrinth.com/v2"

type modrinth struct{}

func (modrinth) Name() string { return "Modrinth" }

type mrHit struct {
	ProjectID   string   `json:"project_id"`
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	IconURL     string   `json:"icon_url"`
	Downloads   int64    `json:"downloads"`
	Categories  []string `json:"categories"`
	ProjectType string   `json:"project_type"`
}

func mrPage(kind, slug string) string {
	t := "mod"
	if kind == "plugin" || kind == "modpack" || kind == "resourcepack" || kind == "shader" {
		t = kind
	}
	return "https://modrinth.com/" + t + "/" + slug
}

func (m mrHit) toProject(kind string) Project {
	id := m.ProjectID
	if id == "" {
		id = m.ID
	}
	return Project{Source: "modrinth", ID: id, Slug: m.Slug, Name: m.Title, Summary: m.Description,
		Author: m.Author, IconURL: m.IconURL, PageURL: mrPage(kind, m.Slug), Downloads: m.Downloads, Categories: m.Categories}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (modrinth) Search(q SearchQuery) ([]Project, int, error) {
	var facets [][]string
	if q.Kind == "modpack" || q.Kind == "resourcepack" || q.Kind == "shader" {
		facets = append(facets, []string{"project_type:" + q.Kind})
	} else if q.Kind == "plugin" {
		facets = append(facets, []string{"project_type:plugin", "project_type:mod"})
	} else {
		facets = append(facets, []string{"project_type:mod"})
	}
	if len(q.Loaders) > 0 && q.Kind != "resourcepack" {
		var l []string
		for _, x := range q.Loaders {
			l = append(l, "categories:"+x)
		}
		facets = append(facets, l)
	}
	if q.MCVersion != "" {
		facets = append(facets, []string{"versions:" + q.MCVersion})
	}
	v := url.Values{}
	v.Set("query", q.Query)
	v.Set("facets", mustJSON(facets))
	v.Set("limit", strconv.Itoa(q.Limit))
	v.Set("offset", strconv.Itoa(q.Offset))
	if q.Query == "" {
		v.Set("index", "downloads")
	} else {
		v.Set("index", "relevance")
	}
	var res struct {
		Hits      []mrHit `json:"hits"`
		TotalHits int     `json:"total_hits"`
	}
	if err := getJSON(modrinthAPI+"/search?"+v.Encode(), nil, 2*time.Minute, &res); err != nil {
		return nil, 0, err
	}
	out := make([]Project, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, h.toProject(q.Kind))
	}
	return out, res.TotalHits, nil
}

func (modrinth) Project(id string) (*Project, error) {
	var p struct {
		ID          string   `json:"id"`
		Slug        string   `json:"slug"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		IconURL     string   `json:"icon_url"`
		Downloads   int64    `json:"downloads"`
		ProjectType string   `json:"project_type"`
		Loaders     []string `json:"loaders"`
		Categories  []string `json:"categories"`
	}
	if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(id), nil, 30*time.Minute, &p); err != nil {
		return nil, err
	}
	kind := "mod"
	if p.ProjectType == "modpack" || p.ProjectType == "resourcepack" || p.ProjectType == "shader" {
		kind = p.ProjectType
	} else if containsAny(p.Loaders, []string{"paper", "spigot", "bukkit", "purpur", "folia", "velocity", "bungeecord", "waterfall"}) && !containsAny(p.Loaders, []string{"fabric", "forge", "neoforge", "quilt"}) {
		kind = "plugin"
	}
	return &Project{Source: "modrinth", ID: p.ID, Slug: p.Slug, Name: p.Title, Summary: p.Description,
		IconURL: p.IconURL, PageURL: mrPage(kind, p.Slug), Downloads: p.Downloads, Categories: p.Categories}, nil
}

type mrVersion struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"`
	DatePublished string   `json:"date_published"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	Files         []struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Primary  bool              `json:"primary"`
		Size     int64             `json:"size"`
		Hashes   map[string]string `json:"hashes"`
	} `json:"files"`
	Dependencies []struct {
		VersionID      string `json:"version_id"`
		ProjectID      string `json:"project_id"`
		FileName       string `json:"file_name"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
}

func (v mrVersion) convert() ModVersion {
	mv := ModVersion{Source: "modrinth", ID: v.ID, ProjectID: v.ProjectID, Name: v.Name, Number: v.VersionNumber,
		Type: v.VersionType, Date: v.DatePublished, GameVersions: v.GameVersions, Loaders: v.Loaders}
	if len(v.Files) > 0 {
		f := v.Files[0]
		for _, x := range v.Files {
			if x.Primary {
				f = x
				break
			}
		}
		mf := &ModFile{URL: f.URL, FileName: f.Filename, Size: f.Size}
		if h := f.Hashes["sha512"]; h != "" {
			mf.Hash = &Hash{"sha512", h}
		} else if h := f.Hashes["sha1"]; h != "" {
			mf.Hash = &Hash{"sha1", h}
		}
		mv.File = mf
	}
	for _, d := range v.Dependencies {
		if d.ProjectID == "" && d.VersionID == "" {
			continue // external file dependency – cannot be resolved
		}
		mv.Deps = append(mv.Deps, Dep{ProjectID: d.ProjectID, VersionID: d.VersionID, Kind: d.DependencyType})
	}
	return mv
}

func (modrinth) Versions(projectID, kind, mc string, loaders []string) ([]ModVersion, error) {
	fetch := func(withMC bool) ([]ModVersion, error) {
		v := url.Values{}
		if len(loaders) > 0 {
			v.Set("loaders", mustJSON(loaders))
		}
		if withMC && mc != "" {
			v.Set("game_versions", mustJSON([]string{mc}))
		}
		var list []mrVersion
		if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(projectID)+"/version?"+v.Encode(), nil, 5*time.Minute, &list); err != nil {
			return nil, err
		}
		out := make([]ModVersion, 0, len(list))
		for _, x := range list {
			out = append(out, x.convert())
		}
		return out, nil
	}
	out, err := fetch(true)
	if err != nil {
		return nil, err
	}
	// Plugins are usually forward compatible and often not tagged with every MC version.
	if len(out) == 0 && (kind == "plugin" || kind == "resourcepack" || kind == "shader") && mc != "" {
		out, err = fetch(false)
		for i := range out {
			out[i].Note = sprintf("nicht ausdrücklich für %s markiert", mc)
		}
	}
	return out, err
}

func (modrinth) Version(projectID, versionID string) (*ModVersion, error) {
	var v mrVersion
	if err := getJSON(modrinthAPI+"/version/"+url.PathEscape(versionID), nil, 30*time.Minute, &v); err != nil {
		return nil, err
	}
	mv := v.convert()
	return &mv, nil
}
