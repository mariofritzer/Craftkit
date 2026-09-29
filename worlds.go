package main

// World backups: zip a world from saves/, keep the newest few, restore on demand.

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxWorldBackups = 5

type World struct {
	Folder     string         `json:"folder"`
	LastPlayed string         `json:"lastPlayed"`
	SizeBytes  int64          `json:"sizeBytes"`
	Backups    []*WorldBackup `json:"backups"`
}

type WorldBackup struct {
	File      string `json:"file"`
	Created   string `json:"created"`
	SizeBytes int64  `json:"sizeBytes"`
	Auto      bool   `json:"auto"`
}

func worldBackupDir(in *Instance, folder string) string {
	return filepath.Join(in.Dir, "craftkit-backups", "worlds", folder)
}

func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func listWorlds(in *Instance) []*World {
	saves := filepath.Join(in.Dir, "saves")
	ents, _ := os.ReadDir(saves)
	seen := map[string]bool{}
	var out []*World
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		level := filepath.Join(saves, e.Name(), "level.dat")
		st, err := os.Stat(level)
		if err != nil {
			continue
		}
		w := &World{Folder: e.Name(), LastPlayed: st.ModTime().Format(time.RFC3339), SizeBytes: dirSize(filepath.Join(saves, e.Name()))}
		w.Backups = listWorldBackups(in, e.Name())
		seen[e.Name()] = true
		out = append(out, w)
	}
	// worlds that only exist as backups (e.g. deleted in game)
	bents, _ := os.ReadDir(filepath.Join(in.Dir, "craftkit-backups", "worlds"))
	for _, e := range bents {
		if e.IsDir() && !seen[e.Name()] {
			if b := listWorldBackups(in, e.Name()); len(b) > 0 {
				out = append(out, &World{Folder: e.Name(), Backups: b})
			}
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].LastPlayed > out[k].LastPlayed })
	return out
}

func listWorldBackups(in *Instance, folder string) []*WorldBackup {
	dir := worldBackupDir(in, folder)
	ents, _ := os.ReadDir(dir)
	out := []*WorldBackup{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, &WorldBackup{File: e.Name(), Created: info.ModTime().Format(time.RFC3339), SizeBytes: info.Size(),
			Auto: strings.Contains(e.Name(), "-auto")})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].File > out[k].File })
	return out
}

func validWorldFolder(name string) error {
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return errors.New("ungültiger Weltname")
	}
	return nil
}

// backupWorld zips saves/<folder> and prunes old backups.
func backupWorld(j *Job, in *Instance, folder string, auto bool) (*WorldBackup, error) {
	if err := validWorldFolder(folder); err != nil {
		return nil, err
	}
	src := filepath.Join(in.Dir, "saves", folder)
	if !fileExists(filepath.Join(src, "level.dat")) {
		return nil, fmt.Errorf("Welt „%s“ nicht gefunden", folder)
	}
	dir := worldBackupDir(in, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := strings.Replace(time.Now().Format("2006-01-02_15-04-05.000"), ".", "-", 1)
	if auto {
		name += "-auto"
	}
	dest := filepath.Join(dir, name+".zip")
	for n := 2; fileExists(dest); n++ {
		dest = filepath.Join(dir, fmt.Sprintf("%s_%d.zip", name, n))
	}
	tmp := dest + ".part"
	total := dirSize(src)
	if err := zipDir(src, tmp, func(done int64) {
		if j != nil && total > 0 {
			j.setStep(fmt.Sprintf("Sichere „%s“ … %d / %d MB", folder, done>>20, total>>20), float64(done)/float64(total))
		}
	}); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return nil, err
	}
	// keep the newest backups only
	list := listWorldBackups(in, folder)
	for i := maxWorldBackups; i < len(list); i++ {
		os.Remove(filepath.Join(dir, list[i].File))
	}
	st, _ := os.Stat(dest)
	b := &WorldBackup{File: filepath.Base(dest), Created: time.Now().Format(time.RFC3339), Auto: auto}
	if st != nil {
		b.SizeBytes = st.Size()
	}
	if j != nil {
		j.logf("Welt „%s“ gesichert (%d MB).", folder, b.SizeBytes>>20)
	}
	return b, nil
}

func zipDir(src, dest string, progress func(done int64)) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	var done int64
	last := time.Now()
	walkErr := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		if rel == "session.lock" {
			return nil // locked while the game runs, not needed
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, _ := zip.FileInfoHeader(info)
		h.Name = rel
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return fmt.Errorf("%s nicht lesbar (läuft das Spiel noch?): %w", rel, err)
		}
		n, err := io.Copy(w, in)
		in.Close()
		done += n
		if progress != nil && time.Since(last) > 200*time.Millisecond {
			progress(done)
			last = time.Now()
		}
		return err
	})
	if err := zw.Close(); err != nil && walkErr == nil {
		walkErr = err
	}
	if err := f.Close(); err != nil && walkErr == nil {
		walkErr = err
	}
	return walkErr
}

// restoreWorld replaces saves/<folder> with a backup. The current state is backed up first.
func restoreWorld(j *Job, in *Instance, folder, file string) error {
	if err := validWorldFolder(folder); err != nil {
		return err
	}
	if file != filepath.Base(file) || !strings.HasSuffix(file, ".zip") {
		return errors.New("ungültige Sicherung")
	}
	zipPath := filepath.Join(worldBackupDir(in, folder), file)
	if !fileExists(zipPath) {
		return errors.New("Sicherung nicht gefunden")
	}
	target := filepath.Join(in.Dir, "saves", folder)
	if fileExists(filepath.Join(target, "level.dat")) {
		j.logf("Sichere zuerst den aktuellen Stand von „%s“ …", folder)
		if _, err := backupWorld(j, in, folder, true); err != nil {
			return fmt.Errorf("aktueller Stand konnte nicht gesichert werden – Wiederherstellung abgebrochen: %w", err)
		}
	}
	// extract into a temp folder first, then swap
	tmp := target + ".craftkit-restore"
	os.RemoveAll(tmp)
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	j.setStep(fmt.Sprintf("Stelle „%s“ wieder her …", folder), -1)
	_, err = extractPrefix(&zr.Reader, "", tmp)
	zr.Close()
	if err != nil {
		os.RemoveAll(tmp)
		return err
	}
	old := target + ".craftkit-old"
	os.RemoveAll(old)
	if fileExists(target) {
		if err := os.Rename(target, old); err != nil {
			os.RemoveAll(tmp)
			return fmt.Errorf("Welt ist in Benutzung (läuft Minecraft noch?): %w", err)
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Rename(old, target)
		return err
	}
	os.RemoveAll(old)
	j.logf("„%s“ wiederhergestellt (Stand %s).", folder, strings.TrimSuffix(file, ".zip"))
	return nil
}

func deleteWorldBackup(in *Instance, folder, file string) error {
	if err := validWorldFolder(folder); err != nil {
		return err
	}
	if file != filepath.Base(file) || !strings.HasSuffix(file, ".zip") {
		return errors.New("ungültige Sicherung")
	}
	return os.Remove(filepath.Join(worldBackupDir(in, folder), file))
}

// autoBackupWorlds backs up recently played worlds before risky changes.
func autoBackupWorlds(j *Job, in *Instance) {
	if !getConfig().autoBackup() {
		return
	}
	var worlds []*World
	var total int64
	for _, w := range listWorlds(in) {
		t, err := time.Parse(time.RFC3339, w.LastPlayed)
		if err != nil || time.Since(t) > 60*24*time.Hour {
			continue
		}
		worlds = append(worlds, w)
		total += w.SizeBytes
	}
	if len(worlds) == 0 {
		return
	}
	if total > 4<<30 {
		j.logf("Welten sind zusammen über 4 GB – automatische Sicherung übersprungen (bei Bedarf im Reiter „Welten“ sichern).")
		return
	}
	j.logf("Sichere %d Welt(en) vor der Änderung …", len(worlds))
	for _, w := range worlds {
		if _, err := backupWorld(j, in, w.Folder, true); err != nil {
			j.logf("Sicherung von „%s“ fehlgeschlagen: %v", w.Folder, err)
		}
	}
}
