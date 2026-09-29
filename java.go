package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func javaExeName() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

// findJava looks for a usable Java runtime without downloading anything.
func findJava() string {
	c := getConfig()
	if c.JavaPath != "" && fileExists(c.JavaPath) {
		return c.JavaPath
	}
	exe := javaExeName()
	var roots []string
	roots = append(roots, filepath.Join(c.MinecraftDir, "runtime"))
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			// Microsoft Store / Xbox app version of the launcher
			roots = append(roots, filepath.Join(la, "Packages", "Microsoft.4297127D64EC6_8wekyb3d8bbwe", "LocalCache", "Local", "runtime"))
		}
		for _, pf := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")} {
			if pf != "" {
				roots = append(roots, filepath.Join(pf, "Minecraft Launcher", "runtime"))
			}
		}
	}
	var found []string
	for _, r := range roots {
		// runtime/<component>/<platform>/<component>/bin/java(.exe)
		m, _ := filepath.Glob(filepath.Join(r, "*", "*", "*", "bin", exe))
		found = append(found, m...)
	}
	if len(found) > 0 {
		// prefer newest runtimes; jre-legacy (Java 8) last
		sort.SliceStable(found, func(i, k int) bool {
			li := strings.Contains(found[i], "jre-legacy")
			lk := strings.Contains(found[k], "jre-legacy")
			if li != lk {
				return lk
			}
			return found[i] > found[k]
		})
		return found[0]
	}
	// previously downloaded portable Java
	if m, _ := filepath.Glob(filepath.Join(dataDir(), "java", "*", "bin", exe)); len(m) > 0 {
		return m[0]
	}
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		p := filepath.Join(jh, "bin", exe)
		if fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath("java"); err == nil {
		return p
	}
	return ""
}

// ensureJava returns a Java executable, downloading a portable JRE if needed.
func ensureJava(j *Job) (string, error) {
	if p := findJava(); p != "" {
		j.logf("Java gefunden: %s", p)
		return p, nil
	}
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("kein Java gefunden – bitte Java 21 installieren oder in den Einstellungen angeben")
	}
	j.logf("Kein Java gefunden – lade eine portable Java-21-Laufzeit (Eclipse Temurin) herunter …")
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	u := "https://api.adoptium.net/v3/binary/latest/21/ga/windows/" + arch + "/jre/hotspot/normal/eclipse?project=jdk"
	zipPath := filepath.Join(dataDir(), "java-download.zip")
	err := download(u, zipPath, nil, func(done, total int64) {
		if total > 0 {
			j.setStep(fmt.Sprintf("Java wird geladen … %d / %d MB", done>>20, total>>20), float64(done)/float64(total)*0.5)
		}
	})
	if err != nil {
		return "", fmt.Errorf("Java-Download fehlgeschlagen: %w", err)
	}
	defer os.Remove(zipPath)
	dest := filepath.Join(dataDir(), "java")
	os.RemoveAll(dest)
	j.setStep("Java wird entpackt …", -1)
	if err := unzip(zipPath, dest); err != nil {
		return "", fmt.Errorf("Java entpacken fehlgeschlagen: %w", err)
	}
	if m, _ := filepath.Glob(filepath.Join(dest, "*", "bin", javaExeName())); len(m) > 0 {
		j.logf("Java installiert: %s", m[0])
		return m[0], nil
	}
	return "", fmt.Errorf("Java wurde geladen, aber java.exe nicht gefunden")
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	destAbs, _ := filepath.Abs(dest)
	for _, f := range r.File {
		p := filepath.Join(destAbs, f.Name)
		if !strings.HasPrefix(p, destAbs+string(os.PathSeparator)) {
			return fmt.Errorf("ungültiger Pfad im Archiv: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(p, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()|0o200)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
