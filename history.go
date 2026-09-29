package main

// Undo: every install/update/remove is recorded as a snapshot. Replaced or removed files are
// moved into the snapshot folder instead of being deleted, so the last change can be rolled back.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxSnapshots = 5

type Snapshot struct {
	ID      string                    `json:"id"`
	Label   string                    `json:"label"`
	Created string                    `json:"created"`
	Items   map[string]*InstalledItem `json:"items"` // manifest before the change
	Moved   []string                  `json:"moved"` // files moved out of the folder (kept in the snapshot dir)
	Added   []string                  `json:"added"` // files the change created
	dir     string
}

func historyDir(t *Target) string {
	if t.Type == "instance" {
		return filepath.Join(filepath.Dir(t.Dir), ".craftkit", "history", t.Kind)
	}
	return filepath.Join(t.Dir, ".craftkit-history")
}

func loadHistory(t *Target) []*Snapshot {
	var list []*Snapshot
	b, err := os.ReadFile(filepath.Join(historyDir(t), "history.json"))
	if err == nil {
		json.Unmarshal(b, &list)
	}
	return list
}

func saveHistory(t *Target, list []*Snapshot) error {
	return writeJSONFile(filepath.Join(historyDir(t), "history.json"), list)
}

func cloneItems(in map[string]*InstalledItem) map[string]*InstalledItem {
	b, _ := json.Marshal(in)
	out := map[string]*InstalledItem{}
	json.Unmarshal(b, &out)
	return out
}

// beginSnapshot starts recording a change. Call commit() when done.
func beginSnapshot(t *Target, label string) *Snapshot {
	id := time.Now().Format("20060102-150405.000")
	id = strings.ReplaceAll(id, ".", "-")
	return &Snapshot{ID: id, Label: label, Created: time.Now().Format(time.RFC3339), Items: cloneItems(t.Items),
		dir: filepath.Join(historyDir(t), id)}
}

// moveOut moves a file out of the target folder into the snapshot instead of deleting it.
func (s *Snapshot) moveOut(t *Target, name string) error {
	src := filepath.Join(t.Dir, name)
	if !fileExists(src) {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, filepath.Join(s.dir, name)); err != nil {
		return err
	}
	s.Moved = append(s.Moved, name)
	return nil
}

func (s *Snapshot) added(name string) { s.Added = append(s.Added, name) }

// commit stores the snapshot (if anything changed) and prunes old ones.
func (s *Snapshot) commit(t *Target, label string) {
	if len(s.Moved) == 0 && len(s.Added) == 0 {
		os.RemoveAll(s.dir)
		return
	}
	if label != "" {
		s.Label = label
	}
	list := append([]*Snapshot{s}, loadHistory(t)...)
	for len(list) > maxSnapshots {
		old := list[len(list)-1]
		os.RemoveAll(filepath.Join(historyDir(t), old.ID))
		list = list[:len(list)-1]
	}
	if err := saveHistory(t, list); err != nil {
		logf("history: %v", err)
	}
}

// rollbackLast undoes the newest snapshot of the target.
func rollbackLast(t *Target) (*Snapshot, error) {
	instMu.Lock()
	defer instMu.Unlock()
	list := loadHistory(t)
	if len(list) == 0 {
		return nil, errNew("es gibt nichts rückgängig zu machen")
	}
	s := list[0]
	dir := filepath.Join(historyDir(t), s.ID)
	for _, name := range s.Added {
		p := filepath.Join(t.Dir, name)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return nil, errf("%s konnte nicht entfernt werden (läuft Minecraft noch?): %w", name, err)
		}
	}
	for _, name := range s.Moved {
		if err := os.Rename(filepath.Join(dir, name), filepath.Join(t.Dir, name)); err != nil && !os.IsNotExist(err) {
			return nil, errf("%s konnte nicht wiederhergestellt werden: %w", name, err)
		}
	}
	t.Items = s.Items
	if t.Items == nil {
		t.Items = map[string]*InstalledItem{}
	}
	if err := t.save(); err != nil {
		return nil, err
	}
	os.RemoveAll(dir)
	recheckLater(t)
	return s, saveHistory(t, list[1:])
}

type HistoryEntry struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Created string `json:"created"`
}

func historyEntries(t *Target) []HistoryEntry {
	var out []HistoryEntry
	for _, s := range loadHistory(t) {
		out = append(out, HistoryEntry{s.ID, s.Label, s.Created})
	}
	sort.SliceStable(out, func(i, k int) bool { return out[i].ID > out[k].ID })
	return out
}
