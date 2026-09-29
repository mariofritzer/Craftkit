//go:build !windows

package main

import (
	"os/exec"
	"runtime"
	"strings"
)

func hideWindow(cmd *exec.Cmd) {}

func openPath(p string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", p).Start()
	}
	return exec.Command("xdg-open", p).Start()
}

func openBrowser(u string) error { return openPath(u) }

func openAppWindow(u string) error { return openBrowser(u) }

func pickFolder(title string) (string, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "`+title+`")`).Output()
		return strings.TrimSpace(string(out)), err
	}
	out, err := exec.Command("zenity", "--file-selection", "--directory", "--title="+title).Output()
	return strings.TrimSpace(string(out)), err
}

func isProcessRunning(names ...string) bool {
	for _, n := range names {
		if exec.Command("pgrep", "-f", n).Run() == nil {
			return true
		}
	}
	return false
}

func detectLauncher() (label, target string) {
	if p := getConfig().LauncherPath; p != "" && fileExists(p) {
		return L("Eigener Pfad"), p
	}
	if runtime.GOOS == "darwin" && fileExists("/Applications/Minecraft.app") {
		return "Minecraft Launcher", "/Applications/Minecraft.app"
	}
	if p, err := exec.LookPath("minecraft-launcher"); err == nil {
		return "Minecraft Launcher", p
	}
	return "", ""
}

func startLauncher() error {
	_, t := detectLauncher()
	if t == "" {
		return errNew("Minecraft Launcher nicht gefunden – bitte Pfad in den Einstellungen angeben")
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", t).Start()
	}
	return exec.Command(t).Start()
}

func focusExistingUI() bool { return false }
