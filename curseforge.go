package main

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const curseforgeAPI = "https://api.curseforge.com/v1"
const cfGameMinecraft = 432
const cfClassMods = 6
const cfClassBukkitPlugins = 5
const cfClassModpacks = 4471

type curseforge struct{ key string }

func (curseforge) Name() string { return "CurseForge" }

func (c curseforge) headers() map[string]string { return map[string]string{"x-api-key": c.key} }

var cfLoaderTypes = map[string]int{"forge": 1, "fabric": 4, "quilt": 5, "neoforge": 6}
var cfLoaderNames = map[string]string{"forge": "forge", "fabric": "fabric", "quilt": "quilt", "neoforge": "neoforge",
	"bukkit": "bukkit", "spigot": "spigot", "paper": "paper"}

type cfMod struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Slug          string  `json:"slug"`
	Summary       string  `json:"summary"`
	DownloadCount float64 `json:"downloadCount"`
	ClassID       int     `json:"classId"`
	Logo          *struct {
		ThumbnailURL string `json:"thumbnailUrl"`
		URL          string `json:"url"`
	} `json:"logo"`
	Links struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
	Categories []struct {
		Name string `json:"name"`
	} `json:"categories"`
}

func (m cfMod) toProject() Project {
	p := Project{Source: "curseforge", ID: strconv.Itoa(m.ID), Slug: m.Slug, Name: m.Name, Summary: m.Summary,
		PageURL: m.Links.WebsiteURL, Downloads: int64(m.DownloadCount)}
	if m.Logo != nil {
		p.IconURL = m.Logo.ThumbnailURL
	}
	if len(m.Authors) > 0 {
		p.Author = m.Authors[0].Name
	}
	for _, c := range m.Categories {
		p.Categories = append(p.Categories, c.Name)
	}
	return p
}

func (c curseforge) Search(q SearchQuery) ([]Project, int, error) {
	v := url.Values{}
	v.Set("gameId", strconv.Itoa(cfGameMinecraft))
	if q.Kind == "modpack" {
		v.Set("classId", strconv.Itoa(cfClassModpacks))
	} else if q.Kind == "resourcepack" {
		v.Set("classId", "12")
	} else if q.Kind == "shader" {
		v.Set("classId", "6552")
	} else if q.Kind == "plugin" {
		v.Set("classId", strconv.Itoa(cfClassBukkitPlugins))
	} else {
		v.Set("classId", strconv.Itoa(cfClassMods))
		if len(q.Loaders) > 0 {
			// Quilt instances also run Fabric mods – most mods on CurseForge are only tagged Fabric.
			l := q.Loaders[0]
			if l == "quilt" {
				l = "fabric"
			}
			if t, ok := cfLoaderTypes[l]; ok {
				v.Set("modLoaderType", strconv.Itoa(t))
			}
		}
	}
	if q.Query != "" {
		v.Set("searchFilter", q.Query)
	}
	if q.MCVersion != "" && q.Kind == "mod" {
		v.Set("gameVersion", q.MCVersion)
	}
	v.Set("sortField", "2") // popularity
	v.Set("sortOrder", "desc")
	v.Set("index", strconv.Itoa(q.Offset))
	v.Set("pageSize", strconv.Itoa(q.Limit))
	var res struct {
		Data       []cfMod `json:"data"`
		Pagination struct {
			TotalCount int `json:"totalCount"`
		} `json:"pagination"`
	}
	if err := getJSON(curseforgeAPI+"/mods/search?"+v.Encode(), c.headers(), 2*time.Minute, &res); err != nil {
		return nil, 0, cfErr(err)
	}
	out := make([]Project, 0, len(res.Data))
	for _, m := range res.Data {
		out = append(out, m.toProject())
	}
	return out, res.Pagination.TotalCount, nil
}

func cfErr(err error) error {
	if he, ok := err.(*HTTPError); ok && (he.Status == 401 || he.Status == 403) {
		return fmt.Errorf("CurseForge lehnt den API-Key ab (HTTP %d) – bitte in den Einstellungen prüfen", he.Status)
	}
	return err
}

func (c curseforge) Project(id string) (*Project, error) {
	var res struct {
		Data cfMod `json:"data"`
	}
	if err := getJSON(curseforgeAPI+"/mods/"+url.PathEscape(id), c.headers(), 30*time.Minute, &res); err != nil {
		return nil, cfErr(err)
	}
	p := res.Data.toProject()
	return &p, nil
}

type cfFile struct {
	ID              int      `json:"id"`
	ModID           int      `json:"modId"`
	DisplayName     string   `json:"displayName"`
	FileName        string   `json:"fileName"`
	ReleaseType     int      `json:"releaseType"`
	FileDate        string   `json:"fileDate"`
	FileLength      int64    `json:"fileLength"`
	FileFingerprint uint32   `json:"fileFingerprint"`
	DownloadURL     string   `json:"downloadUrl"`
	GameVersions    []string `json:"gameVersions"`
	Hashes          []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
	Dependencies []struct {
		ModID        int `json:"modId"`
		RelationType int `json:"relationType"`
	} `json:"dependencies"`
	IsAvailable *bool `json:"isAvailable"`
}

func (c curseforge) convert(f cfFile) ModVersion {
	types := map[int]string{1: "release", 2: "beta", 3: "alpha"}
	mv := ModVersion{Source: "curseforge", ID: strconv.Itoa(f.ID), ProjectID: strconv.Itoa(f.ModID),
		Name: f.DisplayName, Number: f.DisplayName, Type: types[f.ReleaseType], Date: f.FileDate}
	if mv.Type == "" {
		mv.Type = "release"
	}
	for _, gv := range f.GameVersions {
		low := strings.ToLower(gv)
		if l, ok := cfLoaderNames[low]; ok {
			mv.Loaders = append(mv.Loaders, l)
		} else if len(gv) > 0 && gv[0] >= '0' && gv[0] <= '9' {
			mv.GameVersions = append(mv.GameVersions, gv)
		}
	}
	mf := &ModFile{URL: f.DownloadURL, FileName: f.FileName, Size: f.FileLength}
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			mf.Hash = &Hash{"sha1", h.Value}
		}
	}
	if mf.URL == "" {
		// The author disabled third-party downloads – the user has to download it on the website.
		page := ""
		if p, err := c.Project(mv.ProjectID); err == nil {
			page = p.PageURL
		}
		if page != "" {
			mf.ManualURL = page + "/files/" + mv.ID
		} else {
			mf.ManualURL = "https://www.curseforge.com/projects/" + mv.ProjectID
		}
	}
	mv.File = mf
	for _, d := range f.Dependencies {
		kind := ""
		switch d.RelationType {
		case 1:
			kind = "embedded"
		case 2:
			kind = "optional"
		case 3:
			kind = "required"
		case 5:
			kind = "incompatible"
		default:
			continue // tools and includes are not needed at runtime
		}
		mv.Deps = append(mv.Deps, Dep{ProjectID: strconv.Itoa(d.ModID), Kind: kind})
	}
	return mv
}

func (c curseforge) filesPage(projectID, mc string, loaderType int) ([]cfFile, error) {
	v := url.Values{}
	if mc != "" {
		v.Set("gameVersion", mc)
	}
	if loaderType > 0 {
		v.Set("modLoaderType", strconv.Itoa(loaderType))
	}
	v.Set("pageSize", "50")
	var res struct {
		Data []cfFile `json:"data"`
	}
	if err := getJSON(curseforgeAPI+"/mods/"+url.PathEscape(projectID)+"/files?"+v.Encode(), c.headers(), 5*time.Minute, &res); err != nil {
		return nil, cfErr(err)
	}
	return res.Data, nil
}

func (c curseforge) Versions(projectID, kind, mc string, loaders []string) ([]ModVersion, error) {
	var files []cfFile
	seen := map[int]bool{}
	add := func(fs []cfFile) {
		for _, f := range fs {
			if !seen[f.ID] && (f.IsAvailable == nil || *f.IsAvailable) {
				seen[f.ID] = true
				files = append(files, f)
			}
		}
	}
	note := ""
	if kind == "modpack" {
		fs, err := c.filesPage(projectID, "", 0)
		if err != nil {
			return nil, err
		}
		add(fs)
	} else if kind == "plugin" || kind == "resourcepack" || kind == "shader" {
		fs, err := c.filesPage(projectID, mc, 0)
		if err != nil {
			return nil, err
		}
		add(fs)
		if len(files) == 0 && mc != "" {
			fs, err = c.filesPage(projectID, "", 0)
			if err != nil {
				return nil, err
			}
			add(fs)
			note = fmt.Sprintf("nicht ausdrücklich für %s markiert", mc)
		}
	} else {
		for _, l := range loaders {
			t, ok := cfLoaderTypes[l]
			if !ok {
				continue
			}
			fs, err := c.filesPage(projectID, mc, t)
			if err != nil {
				return nil, err
			}
			add(fs)
		}
	}
	sort.SliceStable(files, func(i, k int) bool { return files[i].FileDate > files[k].FileDate })
	out := make([]ModVersion, 0, len(files))
	for _, f := range files {
		mv := c.convert(f)
		mv.Note = note
		out = append(out, mv)
	}
	return out, nil
}

func (c curseforge) Version(projectID, versionID string) (*ModVersion, error) {
	var res struct {
		Data cfFile `json:"data"`
	}
	if err := getJSON(curseforgeAPI+"/mods/"+url.PathEscape(projectID)+"/files/"+url.PathEscape(versionID), c.headers(), 30*time.Minute, &res); err != nil {
		return nil, cfErr(err)
	}
	mv := c.convert(res.Data)
	return &mv, nil
}
