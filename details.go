package main

// Project details (description, gallery, versions with changelog) for the details view.

import (
	"net/url"
	"strconv"
	"time"
)

type GalleryImage struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

type DetailVersion struct {
	ID           string   `json:"id"`
	Number       string   `json:"number"`
	Type         string   `json:"type"`
	Date         string   `json:"date"`
	GameVersions []string `json:"gameVersions"`
	Loaders      []string `json:"loaders"`
	Changelog    string   `json:"changelog,omitempty"`
	ChangelogFmt string   `json:"changelogFmt,omitempty"` // markdown or html
}

type ProjectDetails struct {
	Project
	Body       string            `json:"body"`
	BodyFormat string            `json:"bodyFormat"` // markdown or html
	Gallery    []GalleryImage    `json:"gallery"`
	Links      map[string]string `json:"links"`
	License    string            `json:"license,omitempty"`
	Updated    string            `json:"updated,omitempty"`
	ClientSide string            `json:"clientSide,omitempty"`
	ServerSide string            `json:"serverSide,omitempty"`
	Versions   []DetailVersion   `json:"versions"`
}

func projectDetails(source, id string) (*ProjectDetails, error) {
	switch source {
	case "modrinth":
		var p struct {
			ID          string   `json:"id"`
			Slug        string   `json:"slug"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Body        string   `json:"body"`
			IconURL     string   `json:"icon_url"`
			Downloads   int64    `json:"downloads"`
			ProjectType string   `json:"project_type"`
			Categories  []string `json:"categories"`
			ClientSide  string   `json:"client_side"`
			ServerSide  string   `json:"server_side"`
			Updated     string   `json:"updated"`
			IssuesURL   string   `json:"issues_url"`
			SourceURL   string   `json:"source_url"`
			WikiURL     string   `json:"wiki_url"`
			DiscordURL  string   `json:"discord_url"`
			License     struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"license"`
			Gallery []struct {
				URL      string `json:"url"`
				Title    string `json:"title"`
				Featured bool   `json:"featured"`
			} `json:"gallery"`
		}
		if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(id), nil, 30*time.Minute, &p); err != nil {
			return nil, err
		}
		d := &ProjectDetails{Project: Project{Source: source, ID: p.ID, Slug: p.Slug, Name: p.Title, Summary: p.Description,
			IconURL: p.IconURL, Downloads: p.Downloads, Categories: p.Categories, PageURL: mrPage(p.ProjectType, p.Slug)},
			Body: p.Body, BodyFormat: "markdown", Updated: p.Updated, ClientSide: p.ClientSide, ServerSide: p.ServerSide,
			Links: map[string]string{}}
		if p.License.ID != "" {
			d.License = p.License.ID
		}
		for k, v := range map[string]string{"issues": p.IssuesURL, "source": p.SourceURL, "wiki": p.WikiURL, "discord": p.DiscordURL} {
			if v != "" {
				d.Links[k] = v
			}
		}
		for _, g := range p.Gallery {
			img := GalleryImage{URL: g.URL, Title: g.Title}
			if g.Featured {
				d.Gallery = append([]GalleryImage{img}, d.Gallery...)
			} else {
				d.Gallery = append(d.Gallery, img)
			}
		}
		var vs []struct {
			mrVersion
			Changelog string `json:"changelog"`
		}
		if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(p.ID)+"/version", nil, 10*time.Minute, &vs); err == nil {
			for i, v := range vs {
				if i >= 20 {
					break
				}
				d.Versions = append(d.Versions, DetailVersion{ID: v.ID, Number: v.VersionNumber, Type: v.VersionType, Date: v.DatePublished,
					GameVersions: v.GameVersions, Loaders: v.Loaders, Changelog: v.Changelog, ChangelogFmt: "markdown"})
			}
		}
		return d, nil
	case "curseforge":
		p, err := providerFor("curseforge")
		if err != nil {
			return nil, err
		}
		c := p.(curseforge)
		var res struct {
			Data struct {
				cfMod
				DateModified string `json:"dateModified"`
				Screenshots  []struct {
					URL   string `json:"url"`
					Title string `json:"title"`
				} `json:"screenshots"`
				Links struct {
					WebsiteURL string `json:"websiteUrl"`
					WikiURL    string `json:"wikiUrl"`
					IssuesURL  string `json:"issuesUrl"`
					SourceURL  string `json:"sourceUrl"`
				} `json:"links"`
			} `json:"data"`
		}
		if err := getJSON(curseforgeAPI+"/mods/"+url.PathEscape(id), c.headers(), 30*time.Minute, &res); err != nil {
			return nil, cfErr(err)
		}
		m := res.Data
		proj := m.cfMod.toProject()
		proj.PageURL = m.Links.WebsiteURL
		d := &ProjectDetails{Project: proj, BodyFormat: "html", Updated: m.DateModified, Links: map[string]string{}}
		for k, v := range map[string]string{"issues": m.Links.IssuesURL, "source": m.Links.SourceURL, "wiki": m.Links.WikiURL} {
			if v != "" {
				d.Links[k] = v
			}
		}
		for _, s := range m.Screenshots {
			d.Gallery = append(d.Gallery, GalleryImage{URL: s.URL, Title: s.Title})
		}
		var desc struct {
			Data string `json:"data"`
		}
		if err := getJSON(curseforgeAPI+"/mods/"+url.PathEscape(id)+"/description", c.headers(), 30*time.Minute, &desc); err == nil {
			d.Body = desc.Data
		}
		if files, err := c.filesPage(id, "", 0); err == nil {
			for i, f := range files {
				if i >= 20 {
					break
				}
				mv := c.convert(f)
				d.Versions = append(d.Versions, DetailVersion{ID: mv.ID, Number: mv.Number, Type: mv.Type, Date: mv.Date,
					GameVersions: mv.GameVersions, Loaders: mv.Loaders, ChangelogFmt: "html"})
			}
		}
		return d, nil
	}
	return nil, errUnknownSource
}

// cfChangelog loads a CurseForge file changelog on demand (one request per file).
func cfChangelog(projectID, fileID string) (string, error) {
	p, err := providerFor("curseforge")
	if err != nil {
		return "", err
	}
	var res struct {
		Data string `json:"data"`
	}
	if _, err := strconv.Atoi(fileID); err != nil {
		return "", errUnknownSource
	}
	err = getJSON(curseforgeAPI+"/mods/"+url.PathEscape(projectID)+"/files/"+url.PathEscape(fileID)+"/changelog", p.(curseforge).headers(), time.Hour, &res)
	return res.Data, cfErr(err)
}

var errUnknownSource = errorString("unbekannte Quelle")

type errorString string

func (e errorString) Error() string { return L(string(e)) }
