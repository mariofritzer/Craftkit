//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
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
	allowForeground()
	go bringUIToFront(20 * time.Second)
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
		return L("Eigener Pfad"), p
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
	go bringLauncherToFront(45 * time.Second)
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

// ---------- bringing the UI window to the foreground ----------

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsIconic                 = user32.NewProc("IsIconic")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
)

// allowForeground lets the browser we start take the foreground (we got that right from the user's double click).
func allowForeground() {
	const asfwAny = ^uintptr(0) // ASFW_ANY = (DWORD)-1
	procAllowSetForegroundWindow.Call(asfwAny)
}

func windowText(h uintptr, proc *syscall.LazyProc) string {
	buf := make([]uint16, 256)
	proc.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

// findWindow returns the first visible top-level window matching title/class.
func findWindow(match func(title, class string) bool) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		if v, _, _ := procIsWindowVisible.Call(h); v == 0 {
			if ic, _, _ := procIsIconic.Call(h); ic == 0 {
				return 1
			}
		}
		if match(windowText(h, procGetWindowTextW), windowText(h, procGetClassNameW)) {
			found = h
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// findUIWindow looks for the Edge/Chrome app window showing CraftKit.
func findUIWindow() uintptr {
	return findWindow(func(t, class string) bool {
		return class == "Chrome_WidgetWin_1" && (t == appName || strings.HasPrefix(t, appName+" "))
	})
}

func isLauncherWindow(t, class string) bool {
	return t == "Minecraft Launcher" || strings.HasPrefix(t, "Minecraft Launcher ")
}

// bringLauncherToFront waits for the official launcher window and brings it to the front.
func bringLauncherToFront(timeout time.Duration) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h := findWindow(isLauncherWindow); h != 0 {
			time.Sleep(300 * time.Millisecond)
			forceForeground(h)
			// the launcher sometimes swaps its splash window for the main window
			time.Sleep(1500 * time.Millisecond)
			if h2 := findWindow(isLauncherWindow); h2 != 0 {
				if cur, _, _ := procGetForegroundWindow.Call(); cur != h2 {
					forceForeground(h2)
				}
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// forceForeground brings h to the front, working around Windows' foreground lock.
func forceForeground(h uintptr) bool {
	const swRestore = 9
	if ic, _, _ := procIsIconic.Call(h); ic != 0 {
		procShowWindow.Call(h, swRestore)
	}
	if r, _, _ := procSetForegroundWindow.Call(h); r != 0 {
		return true
	}
	// attach to the thread that currently owns the foreground and try again
	fg, _, _ := procGetForegroundWindow.Call()
	fgThread, _, _ := procGetWindowThreadProcessId.Call(fg, 0)
	me, _, _ := procGetCurrentThreadId.Call()
	if fgThread != 0 && fgThread != me {
		procAttachThreadInput.Call(me, fgThread, 1)
		procBringWindowToTop.Call(h)
		procSetForegroundWindow.Call(h)
		procAttachThreadInput.Call(me, fgThread, 0)
	}
	if cur, _, _ := procGetForegroundWindow.Call(); cur == h {
		return true
	}
	// last resort: at least show it above all other windows once
	const (
		hwndTopmost   = ^uintptr(0)     // -1
		hwndNoTopmost = ^uintptr(0) - 1 // -2
		swpNoMove     = 0x2
		swpNoSize     = 0x1
		swpShow       = 0x40
	)
	procSetWindowPos.Call(h, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShow)
	procSetWindowPos.Call(h, hwndNoTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShow)
	return false
}

// bringUIToFront waits for the UI window to appear and brings it to the front.
func bringUIToFront(timeout time.Duration) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h := findUIWindow(); h != 0 {
			time.Sleep(150 * time.Millisecond) // let the window finish showing
			forceForeground(h)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// focusExistingUI brings an already open CraftKit window to the front; false if none is open.
func focusExistingUI() bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if h := findUIWindow(); h != 0 {
		forceForeground(h)
		return true
	}
	return false
}
