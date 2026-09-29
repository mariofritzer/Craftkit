package main

// Self-update from GitHub releases.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const updateRepo = "mariofritzer/Craftkit"

var githubAPI = "https://api.github.com"

type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	URL       string `json:"url"`   // release page
	Notes     string `json:"notes"` // release body
	ExeURL    string `json:"-"`
	SHAURL    string `json:"-"`
	Published string `json:"published"`
}

func normVersion(v string) string {
	return strings.TrimLeft(strings.TrimSpace(v), "vV")
}

func checkUpdate() (*UpdateInfo, error) {
	info := &UpdateInfo{Current: appVersion}
	var rel struct {
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		Body        string `json:"body"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
		PublishedAt string `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	err := getJSON(githubAPI+"/repos/"+updateRepo+"/releases/latest", map[string]string{"Accept": "application/vnd.github+json"}, 30*time.Minute, &rel)
	if err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.Status == 404 {
			return info, nil // no release yet
		}
		return nil, err
	}
	info.Latest = normVersion(rel.TagName)
	info.URL = rel.HTMLURL
	info.Notes = rel.Body
	info.Published = rel.PublishedAt
	for _, a := range rel.Assets {
		switch strings.ToLower(a.Name) {
		case "craftkit.exe":
			info.ExeURL = a.URL
		case "craftkit.exe.sha256":
			info.SHAURL = a.URL
		}
	}
	cur := normVersion(appVersion)
	info.Available = cur != "dev" && info.ExeURL != "" && info.Latest != "" && compareVersions(info.Latest, cur) > 0
	return info, nil
}

// applyUpdate downloads the new exe, verifies it, swaps it in and starts it.
func applyUpdate(j *Job) (any, error) {
	info, err := checkUpdate()
	if err != nil {
		return nil, err
	}
	if !info.Available {
		return nil, errNew("keine neuere Version verfügbar")
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	self, _ = filepath.EvalSymlinks(self)
	dir := filepath.Dir(self)
	newPath := filepath.Join(dir, ".CraftKit-update.exe")
	j.logf("Lade CraftKit %s …", info.Latest)
	var want *Hash
	if info.SHAURL != "" {
		b, err := getBytes(info.SHAURL, map[string]string{"Accept": "*/*"}, 0)
		if err != nil {
			return nil, errf("Prüfsumme nicht abrufbar: %w", err)
		}
		fields := strings.Fields(string(b))
		if len(fields) == 0 || len(fields[0]) != 64 {
			return nil, errNew("Prüfsummen-Datei ist ungültig")
		}
		want = &Hash{"sha256", fields[0]}
	}
	if err := download(info.ExeURL, newPath, nil, func(done, total int64) {
		if total > 0 {
			j.setStep(sprintf("Lade Update … %d / %d MB", done>>20, total>>20), float64(done)/float64(total)*0.9)
		}
	}); err != nil {
		if os.IsPermission(err) || strings.Contains(err.Error(), "denied") {
			return nil, errf("kein Schreibrecht in %s – lade die neue Version bitte von %s", dir, info.URL)
		}
		return nil, err
	}
	if want != nil {
		got, err := sha256File(newPath)
		if err != nil || !strings.EqualFold(got, want.Value) {
			os.Remove(newPath)
			return nil, errNew("Update-Datei ist beschädigt (Prüfsumme stimmt nicht) – bitte später erneut versuchen")
		}
		j.logf("Prüfsumme ok.")
	}
	old := self + ".old"
	os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		os.Remove(newPath)
		return nil, errf("CraftKit konnte nicht ersetzt werden: %w", err)
	}
	if err := os.Rename(newPath, self); err != nil {
		os.Rename(old, self) // roll back
		return nil, errf("CraftKit konnte nicht ersetzt werden: %w", err)
	}
	j.logf("Update installiert – starte neu …")
	cmd := exec.Command(self, "-after-update")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return nil, errf("Neustart fehlgeschlagen – bitte CraftKit neu öffnen: %w", err)
	}
	go func() {
		time.Sleep(1500 * time.Millisecond) // let the UI receive the result first
		os.Exit(0)
	}()
	return map[string]string{"version": info.Latest}, nil
}

func sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// cleanupOldExe removes the previous exe left behind by an update.
func cleanupOldExe() {
	if self, err := os.Executable(); err == nil {
		for i := 0; i < 10; i++ {
			if err := os.Remove(self + ".old"); err == nil || os.IsNotExist(err) {
				return
			}
			time.Sleep(time.Second)
		}
	}
}
