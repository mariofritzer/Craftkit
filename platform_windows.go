//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func openPath(p string) error {
	return exec.Command("explorer.exe", p).Start()
}

func openBrowser(u string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", u).Start()
}

// openAppWindow opens the UI in a chromeless Edge/Chrome window if possible.
func openAppWindow(u string) error {
	var cands []string
	for _, pf := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if pf == "" {
			continue
		}
		cands = append(cands,
			filepath.Join(pf, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"),
		)
	}
	for _, c := range cands {
		if fileExists(c) {
			profile := filepath.Join(dataDir(), "window")
			cmd := exec.Command(c, "--app="+u, "--user-data-dir="+profile, "--window-size=1280,860", "--no-first-run", "--no-default-browser-check")
			if err := cmd.Start(); err == nil {
				return nil
			}
		}
	}
	return openBrowser(u)
}

func pickFolder(title string) (string, error) {
	title = strings.ReplaceAll(title, "'", "''")
	ps := `Add-Type -AssemblyName System.Windows.Forms;` +
		`$o = New-Object System.Windows.Forms.Form -Property @{TopMost=$true; ShowInTaskbar=$false; WindowState='Minimized'};` +
		`$f = New-Object System.Windows.Forms.FolderBrowserDialog;` +
		`$f.Description = '` + title + `'; $f.ShowNewFolderButton = $true;` +
		`if ($f.ShowDialog($o) -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::OutputEncoding=[Text.Encoding]::UTF8; Write-Output $f.SelectedPath }`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", ps)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func isProcessRunning(names ...string) bool {
	cmd := exec.Command("tasklist.exe", "/FO", "CSV", "/NH")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	s := strings.ToLower(string(out))
	for _, n := range names {
		if strings.Contains(s, "\""+strings.ToLower(n)+"\"") {
			return true
		}
	}
	return false
}

type launcherCandidate struct {
	Label string
	Path  string // exe path, or "shell:" URI
}

func launcherCandidates() []launcherCandidate {
	var c []launcherCandidate
	for _, pf := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")} {
		if pf == "" {
			continue
		}
		c = append(c, launcherCandidate{"Minecraft Launcher", filepath.Join(pf, "Minecraft Launcher", "MinecraftLauncher.exe")})
	}
	// Xbox app installs into <drive>:\XboxGames
	for _, d := range []string{"C", "D", "E", "F"} {
		c = append(c, launcherCandidate{"Minecraft Launcher (Xbox App)", d + `:\XboxGames\Minecraft Launcher\Content\Minecraft.exe`})
	}
	return c
}

// detectLauncher returns a human label and how it will be started.
func detectLauncher() (label, target string) {
	if p := getConfig().LauncherPath; p != "" && fileExists(p) {
		return "Eigener Pfad", p
	}
	for _, c := range launcherCandidates() {
		if fileExists(c.Path) {
			return c.Label, c.Path
		}
	}
	// Microsoft Store version: registered as an app package
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		if fileExists(filepath.Join(la, "Packages", "Microsoft.4297127D64EC6_8wekyb3d8bbwe")) {
			return "Minecraft Launcher (Microsoft Store)", `shell:AppsFolder\Microsoft.4297127D64EC6_8wekyb3d8bbwe!Minecraft`
		}
	}
	return "", ""
}

func startLauncher() error {
	_, target := detectLauncher()
	if target == "" {
		// last try: let Windows resolve the Store app, the user will see an error if missing
		target = `shell:AppsFolder\Microsoft.4297127D64EC6_8wekyb3d8bbwe!Minecraft`
	}
	if strings.HasPrefix(target, "shell:") {
		return exec.Command("explorer.exe", target).Start()
	}
	cmd := exec.Command(target)
	cmd.Dir = filepath.Dir(target)
	return cmd.Start()
}
