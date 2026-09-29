package main

// Background check for available updates across all instances and plugin folders,
// plus a preview of which mods exist for another Minecraft version.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type UpdateHint struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

type UpdateStatus struct {
	Count     int          `json:"count"`
	Items     []UpdateHint `json:"items"`
	CheckedAt string       `json:"checkedAt"`
	Error     string       `json:"error,omitempty"`
}

var (
	updMu       sync.Mutex
	updCache    = map[string]*UpdateStatus{} // "type:id"
	updRunning  bool
	updLastFull time.Time
)

func invalidateUpdates(t *Target) {
	updMu.Lock()
	delete(updCache, t.Type+":"+t.ID)
	updMu.Unlock()
}

// checkTargetUpdates finds newer compatible versions for everything installed in t.
func checkTargetUpdates(t *Target) *UpdateStatus {
	st := &UpdateStatus{CheckedAt: time.Now().Format(time.RFC3339)}
	var mrItems []*InstalledItem
	var cfItems []*InstalledItem
	for _, it := range t.Items {
		switch it.Source {
		case "modrinth":
			mrItems = append(mrItems, it)
		case "curseforge":
			cfItems = append(cfItems, it)
		}
	}
	// Modrinth: one bulk request by file hash
	if len(mrItems) > 0 {
		hashOf := map[string]*InstalledItem{}
		var hashes []string
		for _, it := range mrItems {
			h, _, err := fileSHA1(filepath.Join(t.Dir, it.DiskName()))
			if err != nil {
				continue
			}
			hashOf[h] = it
			hashes = append(hashes, h)
		}
		body := map[string]any{"hashes": hashes, "algorithm": "sha1", "loaders": t.Loaders}
		if t.Kind == "mod" && t.MCVersion != "" {
			body["game_versions"] = []string{t.MCVersion}
		}
		var res map[string]mrVersion
		if err := postJSON(modrinthAPI+"/version_files/update", nil, body, &res); err != nil {
			st.Error = err.Error()
		}
		for h, v := range res {
			it := hashOf[h]
			if it == nil || v.ID == it.VersionID {
				continue
			}
			if it.VersionDate != "" && v.DatePublished <= it.VersionDate {
				continue
			}
			st.Items = append(st.Items, UpdateHint{it.Key, it.Name, it.VersionNumber, v.VersionNumber})
		}
	}
	// CurseForge: per project (usually only a few)
	if len(cfItems) > 0 {
		if p, err := providerFor("curseforge"); err == nil {
			var mu sync.Mutex
			runParallel(len(cfItems), 4, func(i int) {
				it := cfItems[i]
				vs, err := p.Versions(it.ProjectID, t.Kind, t.MCVersion, t.Loaders)
				if err != nil {
					return
				}
				var ok []ModVersion
				for _, x := range vs {
					if compatible(t, &x) {
						ok = append(ok, x)
					}
				}
				best := pickBest(ok)
				if best == nil || best.ID == it.VersionID || (it.VersionDate != "" && best.Date <= it.VersionDate) {
					return
				}
				mu.Lock()
				st.Items = append(st.Items, UpdateHint{it.Key, it.Name, it.VersionNumber, best.Number})
				mu.Unlock()
			})
		}
	}
	sort.Slice(st.Items, func(i, k int) bool { return strings.ToLower(st.Items[i].Name) < strings.ToLower(st.Items[k].Name) })
	st.Count = len(st.Items)
	return st
}

// refreshAllUpdates checks every instance and plugin folder.
func refreshAllUpdates(force bool) {
	updMu.Lock()
	if updRunning || (!force && time.Since(updLastFull) < time.Hour) {
		updMu.Unlock()
		return
	}
	updRunning = true
	updMu.Unlock()
	defer func() {
		updMu.Lock()
		updRunning = false
		updLastFull = time.Now()
		updMu.Unlock()
	}()
	var targets []*Target
	for _, in := range listInstances() {
		for _, typ := range []string{"instance", "instance-rp", "instance-shader"} {
			if t, err := loadTarget(typ, in.ID); err == nil && len(t.Items) > 0 {
				targets = append(targets, t)
			}
		}
	}
	for _, pf := range getConfig().PluginFolders {
		if t, err := loadTarget("plugins", pf.ID); err == nil && len(t.Items) > 0 {
			targets = append(targets, t)
		}
	}
	for _, t := range targets {
		st := checkTargetUpdates(t)
		updMu.Lock()
		updCache[t.Type+":"+t.ID] = st
		updMu.Unlock()
	}
	logf("Update-Prüfung: %d Ziel(e) geprüft", len(targets))
}

func updateSummary() map[string]any {
	updMu.Lock()
	defer updMu.Unlock()
	out := map[string]*UpdateStatus{}
	total := 0
	for k, v := range updCache {
		out[k] = v
		total += v.Count
	}
	return map[string]any{"targets": out, "total": total, "running": updRunning, "checkedAt": updLastFull}
}

func startUpdateWatcher() {
	go func() {
		time.Sleep(4 * time.Second)
		for {
			refreshAllUpdates(false)
			time.Sleep(time.Hour)
		}
	}()
}

// ---------- preview for another version ----------

type PreviewItem struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// previewVersion checks which of the instance's own mods exist for mc/loader.
func previewVersion(id, mc, loader string) ([]PreviewItem, error) {
	in, err := loadInstance(id)
	if err != nil {
		return nil, err
	}
	loaders := modLoadersFor(loader)
	if len(loaders) == 0 {
		return nil, nil
	}
	t := &Target{Kind: "mod", MCVersion: mc, Loaders: loaders}
	var items []*InstalledItem
	for _, it := range in.Items {
		if it.Explicit && !it.Disabled {
			items = append(items, it)
		}
	}
	out := make([]PreviewItem, len(items))
	runParallel(len(items), 6, func(i int) {
		it := items[i]
		pi := PreviewItem{Key: it.Key, Name: it.Name}
		p, err := providerFor(it.Source)
		if err == nil {
			var vs []ModVersion
			vs, err = p.Versions(it.ProjectID, "mod", mc, loaders)
			if err == nil {
				var ok []ModVersion
				for _, x := range vs {
					if compatible(t, &x) {
						ok = append(ok, x)
					}
				}
				if b := pickBest(ok); b != nil {
					pi.Available, pi.Version = true, b.Number
				}
			}
		}
		if err != nil {
			pi.Error = fmt.Sprint(err)
		}
		out[i] = pi
	})
	sort.Slice(out, func(i, k int) bool {
		if out[i].Available != out[k].Available {
			return !out[i].Available
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[k].Name)
	})
	return out, nil
}

// recheckLater refreshes the update status of one target in the background.
func recheckLater(t *Target) {
	if t == nil || inUnitTest {
		return
	}
	cp := *t
	go func() {
		time.Sleep(500 * time.Millisecond)
		fresh, err := loadTarget(cp.Type, cp.ID)
		if err != nil {
			return
		}
		st := checkTargetUpdates(fresh)
		updMu.Lock()
		updCache[cp.Type+":"+cp.ID] = st
		updMu.Unlock()
	}()
}

// inUnitTest disables background network work in unit tests.
var inUnitTest = false
