package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Target is something mods/plugins get installed into: an instance or a plugin folder.
type Target struct {
	Type      string                    `json:"type"` // instance, plugins
	ID        string                    `json:"id"`
	Name      string                    `json:"name"`
	Kind      string                    `json:"kind"` // mod, plugin
	MCVersion string                    `json:"mcVersion"`
	Loaders   []string                  `json:"loaders"`
	Dir       string                    `json:"dir"`
	Items     map[string]*InstalledItem `json:"items"`
	save      func() error
}

type pluginManifestFile struct {
	Items map[string]*InstalledItem `json:"items"`
}

func loadTarget(typ, id string) (*Target, error) {
	switch typ {
	case "instance":
		in, err := loadInstance(id)
		if err != nil {
			return nil, err
		}
		loaders := modLoadersFor(in.Loader)
		if len(loaders) == 0 {
			return nil, fmt.Errorf("Vanilla-Instanzen können keine Mods laden – lege eine Instanz mit Forge, NeoForge, Fabric oder Quilt an")
		}
		t := &Target{Type: typ, ID: id, Name: in.Name, Kind: "mod", MCVersion: in.MCVersion, Loaders: loaders,
			Dir: filepath.Join(in.Dir, "mods"), Items: in.Items}
		t.save = func() error {
			in.Items = t.Items
			return in.save()
		}
		return t, nil
	case "plugins":
		var pf *PluginFolder
		for _, f := range getConfig().PluginFolders {
			if f.ID == id {
				f := f
				pf = &f
			}
		}
		if pf == nil {
			return nil, fmt.Errorf("Plugin-Ordner nicht gefunden")
		}
		mf := &pluginManifestFile{}
		mpath := filepath.Join(pf.Path, pluginManifest)
		if b, err := os.ReadFile(mpath); err == nil {
			json.Unmarshal(b, mf)
		}
		if mf.Items == nil {
			mf.Items = map[string]*InstalledItem{}
		}
		t := &Target{Type: typ, ID: id, Name: pf.Name, Kind: "plugin", MCVersion: pf.MCVersion,
			Loaders: pluginLoadersFor(pf.Platform), Dir: pf.Path, Items: mf.Items}
		t.save = func() error {
			mf.Items = t.Items
			return writeJSONFile(mpath, mf)
		}
		return t, nil
	}
	return nil, fmt.Errorf("unbekanntes Ziel")
}

// ---------- plan ----------

type PlanRequest struct {
	Source    string `json:"source"`
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId,omitempty"`
}

type PlanItem struct {
	Key          string      `json:"key"`
	Source       string      `json:"source"`
	ProjectID    string      `json:"projectId"`
	Slug         string      `json:"slug"`
	Name         string      `json:"name"`
	IconURL      string      `json:"iconUrl"`
	PageURL      string      `json:"pageUrl"`
	Version      *ModVersion `json:"version"`
	Action       string      `json:"action"` // install, update, keep
	Explicit     bool        `json:"explicit"`
	RequiredBy   []string    `json:"requiredBy"`
	Dependencies []string    `json:"dependencies"`
	Incompatible []string    `json:"incompatible,omitempty"`
	Manual       bool        `json:"manual"`
	Note         string      `json:"note,omitempty"`
	FromVersion  string      `json:"fromVersion,omitempty"`
}

type OptionalDep struct {
	Source    string `json:"source"`
	ProjectID string `json:"projectId"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	IconURL   string `json:"iconUrl"`
	For       string `json:"for"`
}

type Plan struct {
	ID        string         `json:"id"`
	Target    *Target        `json:"target"`
	Items     []*PlanItem    `json:"items"`
	Optional  []*OptionalDep `json:"optional"`
	Conflicts []string       `json:"conflicts"`
	Errors    []string       `json:"errors"`
	Warnings  []string       `json:"warnings"`
	Created   time.Time      `json:"-"`
}

var (
	plansMu sync.Mutex
	plans   = map[string]*Plan{}
	planSeq int
)

func storePlan(p *Plan) {
	plansMu.Lock()
	defer plansMu.Unlock()
	planSeq++
	p.ID = strconv.Itoa(planSeq)
	p.Created = time.Now()
	for id, old := range plans {
		if time.Since(old.Created) > time.Hour {
			delete(plans, id)
		}
	}
	plans[p.ID] = p
}

func takePlan(id string) *Plan {
	plansMu.Lock()
	defer plansMu.Unlock()
	p := plans[id]
	delete(plans, id)
	return p
}

type resolver struct {
	provider func(source string) (Provider, error)
	projects map[string]*Project
}

func newResolver() *resolver {
	return &resolver{provider: providerFor, projects: map[string]*Project{}}
}

func itemKey(source, projectID string) string { return source + ":" + projectID }

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func normName(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

func (r *resolver) project(p Provider, source, id string) (*Project, error) {
	k := itemKey(source, id)
	if pr, ok := r.projects[k]; ok {
		return pr, nil
	}
	pr, err := p.Project(id)
	if err != nil {
		return nil, err
	}
	r.projects[k] = pr
	return pr, nil
}

func compatible(t *Target, v *ModVersion) bool {
	if len(v.Loaders) > 0 && len(t.Loaders) > 0 && !containsAny(v.Loaders, t.Loaders) {
		return false
	}
	if t.Kind == "mod" && t.MCVersion != "" && len(v.GameVersions) > 0 && !contains(v.GameVersions, t.MCVersion) {
		return false
	}
	return true
}

type queued struct {
	source, projectID, versionID string
	explicit                     bool
	requiredBy                   string
}

// resolve builds an installation plan including all required dependencies.
func (r *resolver) resolve(t *Target, reqs []PlanRequest) *Plan {
	plan := &Plan{Target: t}
	byKey := map[string]*PlanItem{}
	type incompat struct{ key, by string }
	var incompats []incompat
	optSeen := map[string]bool{}

	// installed items by normalised name, to notice the same mod from another source
	installedByName := map[string]*InstalledItem{}
	for _, it := range t.Items {
		installedByName[normName(it.Name)] = it
		if it.Slug != "" {
			installedByName[normName(it.Slug)] = it
		}
	}

	var queue []queued
	for _, rq := range reqs {
		queue = append(queue, queued{rq.Source, rq.ProjectID, rq.VersionID, true, ""})
	}

	for len(queue) > 0 {
		q := queue[0]
		queue = queue[1:]
		if len(byKey) > 400 {
			plan.Errors = append(plan.Errors, "Zu viele Abhängigkeiten – Abbruch.")
			break
		}
		p, err := r.provider(q.source)
		if err != nil {
			plan.Errors = append(plan.Errors, err.Error())
			continue
		}
		// Modrinth may name a dependency only by version id
		var pinned *ModVersion
		if q.projectID == "" && q.versionID != "" {
			v, err := p.Version("", q.versionID)
			if err != nil {
				plan.Errors = append(plan.Errors, fmt.Sprintf("Abhängigkeit von %s nicht gefunden: %v", q.requiredBy, err))
				continue
			}
			q.projectID = v.ProjectID
			pinned = v
		}
		key := itemKey(q.source, q.projectID)
		if it, ok := byKey[key]; ok {
			if q.requiredBy != "" && !contains(it.RequiredBy, q.requiredBy) {
				it.RequiredBy = append(it.RequiredBy, q.requiredBy)
			}
			if q.explicit {
				it.Explicit = true
			}
			continue
		}
		proj, err := r.project(p, q.source, q.projectID)
		if err != nil {
			who := "Auswahl"
			if q.requiredBy != "" {
				who = q.requiredBy
			}
			plan.Errors = append(plan.Errors, fmt.Sprintf("Projekt %s (%s) nicht abrufbar: %v", q.projectID, who, err))
			continue
		}
		item := &PlanItem{Key: key, Source: q.source, ProjectID: q.projectID, Slug: proj.Slug, Name: proj.Name,
			IconURL: proj.IconURL, PageURL: proj.PageURL, Explicit: q.explicit}
		if q.requiredBy != "" {
			item.RequiredBy = []string{q.requiredBy}
		}
		installed := t.Items[key]
		if installed == nil && !q.explicit {
			// same mod already installed from the other source?
			for _, n := range []string{normName(proj.Name), normName(proj.Slug)} {
				if other, ok := installedByName[n]; ok && n != "" {
					item.Action = "keep"
					item.Note = "bereits über " + sourceNames[other.Source] + " installiert"
					item.Version = &ModVersion{Number: other.VersionNumber}
					installed = other
					break
				}
			}
			if item.Action == "keep" {
				byKey[key] = item
				plan.Items = append(plan.Items, item)
				continue
			}
		}
		if installed == nil && q.explicit {
			for _, n := range []string{normName(proj.Name), normName(proj.Slug)} {
				if other, ok := installedByName[n]; ok && n != "" && other.Source != q.source {
					plan.Warnings = append(plan.Warnings, fmt.Sprintf("„%s“ ist schon über %s installiert – dann wäre die Mod doppelt vorhanden.", proj.Name, sourceNames[other.Source]))
					break
				}
			}
		}
		// dependency that is already installed: keep what is there
		if installed != nil && !q.explicit {
			item.Action = "keep"
			item.Version = &ModVersion{ID: installed.VersionID, Number: installed.VersionNumber}
			item.Note = "bereits installiert"
			byKey[key] = item
			plan.Items = append(plan.Items, item)
			continue
		}

		// choose version
		var v *ModVersion
		installedCompatible := true // does the installed version still fit the target's MC version?
		if q.versionID != "" && pinned == nil {
			pv, err := p.Version(q.projectID, q.versionID)
			if err == nil {
				pinned = pv
			}
		}
		if pinned != nil && (q.explicit || compatible(t, pinned)) {
			v = pinned
		} else {
			vs, err := p.Versions(q.projectID, t.Kind, t.MCVersion, t.Loaders)
			if err != nil {
				plan.Errors = append(plan.Errors, fmt.Sprintf("Versionen von „%s“ nicht abrufbar: %v", proj.Name, err))
				continue
			}
			var ok []ModVersion
			for _, x := range vs {
				if compatible(t, &x) {
					ok = append(ok, x)
				}
			}
			v = pickBest(ok)
			if installed != nil {
				installedCompatible = false
				for _, x := range ok {
					if x.ID == installed.VersionID {
						installedCompatible = true
					}
				}
			}
		}
		if v == nil {
			msg := fmt.Sprintf("Keine passende Version von „%s“ für %s %s gefunden", proj.Name, strings.Join(t.Loaders, "/"), t.MCVersion)
			if q.requiredBy != "" {
				msg += fmt.Sprintf(" (wird von „%s“ benötigt)", q.requiredBy)
			}
			if installed != nil {
				msg += " Die installierte Version bleibt, passt aber evtl. nicht."
			}
			plan.Errors = append(plan.Errors, msg+".")
			continue
		}
		item.Version = v
		item.Note = v.Note
		if v.File == nil {
			plan.Errors = append(plan.Errors, fmt.Sprintf("„%s“ %s hat keine Datei zum Herunterladen.", proj.Name, v.Number))
			continue
		}
		item.Manual = v.File.URL == ""

		switch {
		case installed == nil:
			item.Action = "install"
		case installed.VersionID == v.ID:
			item.Action = "keep"
			item.Note = "bereits aktuell"
		case q.versionID == "" && installedCompatible && installed.InstalledVersionDate() != "" && v.Date <= installed.InstalledVersionDate():
			item.Action = "keep"
			item.Note = "bereits aktuell"
		default:
			item.Action = "update"
			item.FromVersion = installed.VersionNumber
			if !installedCompatible {
				item.Note = "installierte Version passt nicht zu " + t.MCVersion
			}
		}
		byKey[key] = item
		plan.Items = append(plan.Items, item)

		// dependencies (for kept items we still record them so removal checks work)
		for _, d := range v.Deps {
			dk := itemKey(q.source, d.ProjectID)
			switch d.Kind {
			case "required":
				if d.ProjectID != "" {
					item.Dependencies = append(item.Dependencies, dk)
				}
				queue = append(queue, queued{q.source, d.ProjectID, d.VersionID, false, proj.Name})
			case "optional":
				if d.ProjectID == "" || optSeen[dk] || t.Items[dk] != nil {
					continue
				}
				optSeen[dk] = true
				od := &OptionalDep{Source: q.source, ProjectID: d.ProjectID, Key: dk, For: proj.Name}
				if op, err := r.project(p, q.source, d.ProjectID); err == nil {
					od.Name = op.Name
					od.IconURL = op.IconURL
				} else {
					od.Name = d.ProjectID
				}
				plan.Optional = append(plan.Optional, od)
			case "incompatible":
				if d.ProjectID != "" {
					incompats = append(incompats, incompat{dk, proj.Name})
					item.Incompatible = append(item.Incompatible, dk)
				}
			}
		}
	}

	// optional deps that ended up in the plan anyway are not optional
	var opts []*OptionalDep
	for _, o := range plan.Optional {
		if _, inPlan := byKey[o.Key]; !inPlan {
			opts = append(opts, o)
		}
	}
	plan.Optional = opts

	for _, ic := range incompats {
		name := ""
		if it, ok := byKey[ic.key]; ok {
			name = it.Name
		} else if it, ok := t.Items[ic.key]; ok {
			name = it.Name
		}
		if name != "" {
			plan.Conflicts = append(plan.Conflicts, fmt.Sprintf("„%s“ ist laut Autor nicht mit „%s“ kompatibel.", ic.by, name))
		}
	}
	// installed mods that declared the new ones incompatible
	for _, inst := range t.Items {
		if pi, ok := byKey[inst.Key]; ok && pi.Action != "keep" {
			continue // being replaced, its new version was checked above
		}
		for _, bad := range inst.Incompatible {
			if pi, ok := byKey[bad]; ok && pi.Action != "keep" {
				plan.Conflicts = append(plan.Conflicts, fmt.Sprintf("Das installierte „%s“ ist laut Autor nicht mit „%s“ kompatibel.", inst.Name, pi.Name))
			}
		}
	}

	// explicit items first, then dependencies, alphabetically
	sort.SliceStable(plan.Items, func(i, k int) bool {
		a, b := plan.Items[i], plan.Items[k]
		if a.Explicit != b.Explicit {
			return a.Explicit
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return plan
}

// InstalledVersionDate returns the stored publish date of the installed version.
func (it *InstalledItem) InstalledVersionDate() string { return it.VersionDate }

// resolveUpdateAll checks every installed item for a newer version.
func (r *resolver) resolveUpdateAll(t *Target, reqs []PlanRequest) *Plan {
	plan := r.resolve(t, reqs)
	for _, it := range plan.Items {
		if ex := t.Items[it.Key]; ex != nil {
			it.Explicit = ex.Explicit
		}
	}
	return plan
}

// ---------- apply / remove ----------

type ApplyResult struct {
	Installed []string    `json:"installed"`
	Updated   []string    `json:"updated"`
	Manual    []*PlanItem `json:"manual"`
	Failed    []string    `json:"failed"`
	Dir       string      `json:"dir"`
}

func applyPlan(j *Job, plan *Plan) (*ApplyResult, error) {
	instMu.Lock()
	defer instMu.Unlock()
	// reload target so we write on top of the newest manifest
	t, err := loadTarget(plan.Target.Type, plan.Target.ID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return nil, err
	}
	res := &ApplyResult{Dir: t.Dir}
	var todo []*PlanItem
	for _, it := range plan.Items {
		if it.Action == "keep" {
			if ex := t.Items[it.Key]; ex != nil && it.Explicit && !ex.Explicit {
				ex.Explicit = true
			}
			continue
		}
		if it.Manual {
			res.Manual = append(res.Manual, it)
			continue
		}
		todo = append(todo, it)
	}
	for i, it := range todo {
		base := float64(i) / float64(len(todo))
		span := 1 / float64(len(todo))
		j.setStep(fmt.Sprintf("Lade %s (%d/%d) …", it.Name, i+1, len(todo)), base)
		f := it.Version.File
		name := safeFileName(f.FileName)
		dest := filepath.Join(t.Dir, name)
		err := download(f.URL, dest, f.Hash, func(done, total int64) {
			if total > 0 {
				j.setProgress(base + span*float64(done)/float64(total))
			}
		})
		if err != nil {
			j.logf("✗ %s: %v", it.Name, err)
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", it.Name, err))
			continue
		}
		prev := t.Items[it.Key]
		if prev != nil && prev.FileName != "" && !strings.EqualFold(prev.FileName, name) {
			os.Remove(filepath.Join(t.Dir, prev.FileName))
		}
		explicit := it.Explicit || (prev != nil && prev.Explicit)
		t.Items[it.Key] = &InstalledItem{
			Key: it.Key, Source: it.Source, ProjectID: it.ProjectID, Slug: it.Slug, Name: it.Name,
			IconURL: it.IconURL, PageURL: it.PageURL, VersionID: it.Version.ID, VersionNumber: it.Version.Number,
			VersionDate: it.Version.Date, FileName: name, Explicit: explicit, Dependencies: it.Dependencies,
			Incompatible: it.Incompatible,
			InstalledAt:  time.Now().Format(time.RFC3339),
		}
		if err := t.save(); err != nil {
			return nil, err
		}
		if it.Action == "update" {
			j.logf("↻ %s aktualisiert: %s → %s", it.Name, it.FromVersion, it.Version.Number)
			res.Updated = append(res.Updated, it.Name)
		} else {
			tag := ""
			if !it.Explicit && len(it.RequiredBy) > 0 {
				tag = " (benötigt von " + strings.Join(it.RequiredBy, ", ") + ")"
			}
			j.logf("✓ %s %s%s", it.Name, it.Version.Number, tag)
			res.Installed = append(res.Installed, it.Name)
		}
	}
	if err := t.save(); err != nil {
		return nil, err
	}
	for _, m := range res.Manual {
		j.logf("⚠ %s muss manuell von der Website geladen werden: %s", m.Name, m.Version.File.ManualURL)
	}
	if len(res.Failed) > 0 && len(res.Installed)+len(res.Updated) == 0 {
		return res, fmt.Errorf("keine Datei konnte installiert werden")
	}
	return res, nil
}

// dependents returns names of installed items that require key.
func dependents(t *Target, key string) []string {
	var out []string
	for _, it := range t.Items {
		if it.Key != key && contains(it.Dependencies, key) {
			out = append(out, it.Name)
		}
	}
	sort.Strings(out)
	return out
}

// orphansAfterRemoval lists auto-installed dependencies nobody needs once key is gone.
func orphansAfterRemoval(t *Target, key string) []*InstalledItem {
	remaining := map[string]*InstalledItem{}
	for k, v := range t.Items {
		if k != key {
			remaining[k] = v
		}
	}
	var orphans []*InstalledItem
	for {
		changed := false
		for k, it := range remaining {
			if it.Explicit {
				continue
			}
			needed := false
			for k2, o := range remaining {
				if k2 != k && contains(o.Dependencies, k) {
					needed = true
					break
				}
			}
			if !needed {
				orphans = append(orphans, it)
				delete(remaining, k)
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	sort.Slice(orphans, func(i, k int) bool { return orphans[i].Name < orphans[k].Name })
	return orphans
}

func removeItem(t *Target, key string, withOrphans bool) ([]string, error) {
	instMu.Lock()
	defer instMu.Unlock()
	it := t.Items[key]
	if it == nil {
		return nil, fmt.Errorf("nicht installiert")
	}
	var victims []*InstalledItem
	if withOrphans {
		victims = orphansAfterRemoval(t, key)
	}
	victims = append([]*InstalledItem{it}, victims...)
	var removed []string
	for _, v := range victims {
		if v.FileName != "" {
			p := filepath.Join(t.Dir, v.FileName)
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return removed, fmt.Errorf("%s konnte nicht gelöscht werden (läuft Minecraft noch?): %w", v.FileName, err)
			}
		}
		delete(t.Items, v.Key)
		removed = append(removed, v.Name)
	}
	return removed, t.save()
}
