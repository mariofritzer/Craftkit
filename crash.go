package main

// Crash help: read the newest crash report / game log and point at the likely cause.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type CrashMod struct {
	Name     string `json:"name"`
	ModID    string `json:"modId,omitempty"`
	Key      string `json:"key,omitempty"`  // managed item key
	File     string `json:"file,omitempty"` // jar file name
	Disabled bool   `json:"disabled,omitempty"`
	Hits     int    `json:"hits,omitempty"`
}

type CrashFinding struct {
	Kind    string        `json:"kind"` // missing, incompatible, suspect, memory, java, mixin, info
	Title   string        `json:"title"`
	Detail  string        `json:"detail,omitempty"`
	Mods    []CrashMod    `json:"mods,omitempty"`
	Missing []*MissingDep `json:"missing,omitempty"`
}

type CrashReport struct {
	Found       bool           `json:"found"`
	File        string         `json:"file,omitempty"`
	Time        string         `json:"time,omitempty"`
	Description string         `json:"description,omitempty"`
	Findings    []CrashFinding `json:"findings"`
	Excerpt     string         `json:"excerpt,omitempty"`
}

var (
	reFabricMissing   = regexp.MustCompile(`(?i)Mod '([^']+)' \(([\w\-.]+)\) [\w.+\-]+ requires (?:any version|version [^ ]+(?: or later)?|[^ ]+) of (?:mod )?'?([^',!]+)'?(?: \(([\w\-.]+)\))?,? which is missing`)
	reFabricWrongVer  = regexp.MustCompile(`(?i)Mod '([^']+)' \(([\w\-.]+)\) [\w.+\-]+ requires (.+?) of (?:mod )?'?([^',]+?)'?(?: \(([\w\-.]+)\))?, but only the wrong version is present: (.+?)!`)
	reFabricBreaks    = regexp.MustCompile(`(?i)Mod '([^']+)' \(([\w\-.]+)\) [\w.+\-]+ is incompatible with (?:any version of |version [^ ]+ of )?(?:mod )?'?([^',]+?)'?(?: \(([\w\-.]+)\))?[,!]`)
	reForgeMissing    = regexp.MustCompile(`(?i)Mod ID: '([\w\-.]+)', Requested by: '([\w\-.]+)', Expected range: '([^']*)', Actual version: '([^']*)'`)
	reMixinMod        = regexp.MustCompile(`(?i)(?:Mixin apply(?: for mod)? ([\w\-.]+) failed|from mod ([\w\-.]+)\]|mixins?\.([\w\-]+)\.json)`)
	reSuspectedHeader = regexp.MustCompile(`(?i)^\s*Suspected Mods?:\s*(.*)$`)
	reDescription     = regexp.MustCompile(`^Description:\s*(.+)$`)
)

func latestFile(dir string, match func(name string) bool) (string, time.Time) {
	ents, _ := os.ReadDir(dir)
	var best string
	var bt time.Time
	for _, e := range ents {
		if e.IsDir() || !match(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err == nil && info.ModTime().After(bt) {
			best, bt = filepath.Join(dir, e.Name()), info.ModTime()
		}
	}
	return best, bt
}

func readLimited(p string, max int64) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, _ := f.Stat()
	if st != nil && st.Size() > max {
		f.Seek(st.Size()-max, 0) // keep the end, that is where errors are
	}
	b := make([]byte, max)
	n, _ := f.Read(b)
	return string(b[:n])
}

// analyzeCrash looks at the newest crash report and the game log of an instance.
func analyzeCrash(in *Instance) *CrashReport {
	rep := &CrashReport{Findings: []CrashFinding{}}
	crashFile, crashTime := latestFile(filepath.Join(in.Dir, "crash-reports"), func(n string) bool { return strings.HasSuffix(n, ".txt") })
	logFile, logTime := filepath.Join(in.Dir, "logs", "latest.log"), time.Time{}
	if st, err := os.Stat(logFile); err == nil {
		logTime = st.ModTime()
	} else {
		logFile = ""
	}
	var text string
	switch {
	case crashFile != "" && (logFile == "" || !crashTime.Before(logTime.Add(-2*time.Minute))):
		rep.File, rep.Time, text = crashFile, crashTime.Format(time.RFC3339), readLimited(crashFile, 512<<10)
		// the log often has the dependency messages that led to the crash
		if logFile != "" {
			text += "\n" + readLimited(logFile, 256<<10)
		}
	case logFile != "":
		text = readLimited(logFile, 512<<10)
		if !looksLikeFailure(text) {
			if crashFile != "" {
				rep.File, rep.Time, text = crashFile, crashTime.Format(time.RFC3339), readLimited(crashFile, 512<<10)
			} else {
				return rep // nothing crashed
			}
		} else {
			rep.File, rep.Time = logFile, logTime.Format(time.RFC3339)
		}
	default:
		return rep
	}
	rep.Found = true

	jars := scanFolder(filepath.Join(in.Dir, "mods"))
	byID := map[string]*JarInfo{}
	for _, j := range jars {
		for _, id := range append([]string{j.ModID}, j.Provides...) {
			if id != "" {
				byID[strings.ToLower(id)] = j
			}
		}
	}
	modFor := func(id, fallbackName string) CrashMod {
		cm := CrashMod{Name: fallbackName, ModID: id}
		if j := byID[strings.ToLower(id)]; j != nil {
			cm.Name, cm.File, cm.Disabled = j.Name, j.File, j.Disabled
			for _, it := range in.Items {
				if strings.EqualFold(it.FileName, strings.TrimSuffix(j.File, ".disabled")) {
					cm.Key, cm.Name = it.Key, it.Name
				}
			}
		}
		if cm.Name == "" {
			cm.Name = id
		}
		return cm
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	missing := map[string]*MissingDep{}
	var missingOrder []string
	addMissing := func(id, by string) {
		lid := strings.ToLower(id)
		if builtinIDs[lid] {
			return
		}
		m := missing[lid]
		if m == nil {
			m = &MissingDep{ModID: id}
			missing[lid] = m
			missingOrder = append(missingOrder, lid)
		}
		if by != "" && !contains(m.NeededBy, by) {
			m.NeededBy = append(m.NeededBy, by)
		}
	}
	seenFinding := map[string]bool{}
	add := func(f CrashFinding) {
		k := f.Kind + "|" + f.Title
		if !seenFinding[k] {
			seenFinding[k] = true
			rep.Findings = append(rep.Findings, f)
		}
	}
	scores := map[string]int{}
	var suspects []CrashMod
	inSuspected := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if m := reDescription.FindStringSubmatch(t); m != nil && rep.Description == "" {
			rep.Description = m[1]
		}
		if m := reFabricMissing.FindStringSubmatch(t); m != nil {
			id := m[4]
			if id == "" {
				id = m[3]
			}
			addMissing(id, m[1])
			continue
		}
		if m := reFabricWrongVer.FindStringSubmatch(t); m != nil {
			target := m[5]
			if target == "" {
				target = m[4]
			}
			if strings.EqualFold(target, "minecraft") || strings.EqualFold(target, "fabricloader") || strings.EqualFold(target, "java") {
				what := map[string]string{"minecraft": "Minecraft-Version", "fabricloader": "Fabric-Loader-Version", "java": "Java-Version"}[strings.ToLower(target)]
				add(CrashFinding{Kind: "incompatible", Title: fmt.Sprintf("„%s“ passt nicht zur %s", m[1], what),
					Detail: fmt.Sprintf("Benötigt %s, vorhanden ist %s. Aktualisiere die Mod oder wähle eine passende Version.", m[3], m[6]),
					Mods:   []CrashMod{modFor(m[2], m[1])}})
			} else {
				add(CrashFinding{Kind: "incompatible", Title: fmt.Sprintf("„%s“ braucht eine andere Version von „%s“", m[1], m[4]),
					Detail: fmt.Sprintf("Benötigt %s, vorhanden ist %s.", m[3], m[6]),
					Mods:   []CrashMod{modFor(target, m[4]), modFor(m[2], m[1])}})
			}
			continue
		}
		if m := reFabricBreaks.FindStringSubmatch(t); m != nil {
			other := m[4]
			if other == "" {
				other = m[3]
			}
			add(CrashFinding{Kind: "incompatible", Title: fmt.Sprintf("„%s“ verträgt sich nicht mit „%s“", m[1], m[3]),
				Detail: "Deaktiviere eine der beiden Mods.", Mods: []CrashMod{modFor(m[2], m[1]), modFor(other, m[3])}})
			continue
		}
		if m := reForgeMissing.FindStringSubmatch(t); m != nil {
			if m[4] == "[MISSING]" || m[4] == "" {
				addMissing(m[1], modFor(m[2], m[2]).Name)
			} else {
				add(CrashFinding{Kind: "incompatible", Title: fmt.Sprintf("„%s“ braucht eine andere Version von „%s“", modFor(m[2], m[2]).Name, m[1]),
					Detail: fmt.Sprintf("Erwartet %s, vorhanden ist %s.", m[3], m[4]), Mods: []CrashMod{modFor(m[1], m[1]), modFor(m[2], m[2])}})
			}
			continue
		}
		if m := reSuspectedHeader.FindStringSubmatch(t); m != nil {
			inSuspected = true
			if cm, ok := suspectFromLine(m[1], modFor); ok {
				suspects = append(suspects, cm)
			}
			continue
		}
		if inSuspected {
			if t == "" || strings.HasPrefix(t, "Stacktrace") || strings.HasPrefix(t, "--") {
				inSuspected = false
			} else if cm, ok := suspectFromLine(t, modFor); ok {
				suspects = append(suspects, cm)
			}
			continue
		}
		low := strings.ToLower(t)
		switch {
		case strings.Contains(low, "java.lang.outofmemoryerror") || strings.Contains(low, "java heap space"):
			add(CrashFinding{Kind: "memory", Title: "Zu wenig Arbeitsspeicher",
				Detail: "Minecraft hatte nicht genug RAM. Erhöhe den Arbeitsspeicher der Instanz (Bearbeiten → RAM), z. B. auf 6–8 GB bei vielen Mods."})
		case strings.Contains(low, "unsupportedclassversionerror") || strings.Contains(low, "has been compiled by a more recent version of the java runtime"):
			add(CrashFinding{Kind: "java", Title: "Falsche Java-Version",
				Detail: "Eine Mod braucht eine neuere Java-Version als die, mit der Minecraft gestartet wurde. Meist hilft ein Update der Mod-Loader-Version oder das Entfernen einer Mod, die für eine neuere Minecraft-Version gebaut ist."})
		case strings.Contains(low, "mixin") && (strings.Contains(low, "failed") || strings.Contains(low, "error")):
			if mm := reMixinMod.FindStringSubmatch(t); mm != nil {
				id := firstNonEmpty(mm[1], mm[2], mm[3])
				if cm := modFor(id, id); cm.File != "" {
					add(CrashFinding{Kind: "mixin", Title: fmt.Sprintf("„%s“ konnte nicht geladen werden", cm.Name),
						Detail: "Die Mod greift in Minecraft ein und ist mit dieser Version oder einer anderen Mod nicht verträglich. Aktualisiere oder deaktiviere sie.",
						Mods:   []CrashMod{cm}})
				}
			}
		}
		// stack frames mentioning a mod jar or mod id
		if strings.HasPrefix(t, "at ") || strings.Contains(t, "~[") || strings.Contains(t, "{re:") {
			for _, j := range jars {
				base := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(j.File, ".disabled"), ".jar"))
				if base != "" && strings.Contains(low, base) {
					scores[j.File]++
				} else if j.ModID != "" && len(j.ModID) > 3 && strings.Contains(low, "."+strings.ToLower(j.ModID)+".") {
					scores[j.File]++
				}
			}
		}
	}
	if len(missingOrder) > 0 {
		var ms []*MissingDep
		for _, id := range missingOrder {
			ms = append(ms, missing[id])
		}
		suggestForMissing(ms)
		var names []string
		for _, m := range ms {
			names = append(names, firstNonEmpty(m.Name, m.ModID))
		}
		rep.Findings = append([]CrashFinding{{Kind: "missing", Title: "Fehlende Abhängigkeiten: " + strings.Join(names, ", "),
			Detail: "Diese Mods werden von anderen Mods benötigt, sind aber nicht installiert.", Missing: ms}}, rep.Findings...)
	}
	if len(suspects) > 0 {
		add(CrashFinding{Kind: "suspect", Title: "Der Absturzbericht verdächtigt: " + joinModNames(suspects),
			Detail: "Versuche zuerst, diese Mod zu aktualisieren. Hilft das nicht, deaktiviere sie und starte erneut.", Mods: suspects})
	}
	if len(scores) > 0 && len(suspects) == 0 {
		type sc struct {
			file string
			n    int
		}
		var list []sc
		for f, n := range scores {
			list = append(list, sc{f, n})
		}
		sort.Slice(list, func(i, k int) bool { return list[i].n > list[k].n })
		var mods []CrashMod
		for i, s := range list {
			if i >= 3 {
				break
			}
			for _, j := range jars {
				if j.File == s.file {
					cm := modFor(j.ModID, j.Name)
					cm.Hits = s.n
					mods = append(mods, cm)
				}
			}
		}
		add(CrashFinding{Kind: "suspect", Title: "Im Fehlerverlauf tauchen auf: " + joinModNames(mods),
			Detail: "Diese Mods waren am Absturz beteiligt. Das heißt nicht sicher, dass sie schuld sind – aktualisieren oder testweise deaktivieren hilft beim Eingrenzen.", Mods: mods})
	}
	if len(rep.Findings) == 0 {
		add(CrashFinding{Kind: "info", Title: "Keine eindeutige Ursache erkannt",
			Detail: "Tipp: Deaktiviere zuletzt hinzugefügte Mods oder nutze „Rückgängig“, um den Stand vor der letzten Änderung wiederherzustellen."})
	}
	rep.Excerpt = excerpt(lines)
	return rep
}

var reSuspectLine = regexp.MustCompile(`^(.+?) \(([\w\-.]+)\)`)

// suspectFromLine parses "Sodium (sodium), Version: 0.6.0"; other lines (issue tracker URLs …) are ignored.
func suspectFromLine(line string, modFor func(id, name string) CrashMod) (CrashMod, bool) {
	m := reSuspectLine.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil || strings.Contains(line, "URL:") {
		return CrashMod{}, false
	}
	return modFor(m[2], strings.TrimSpace(m[1])), true
}

func joinModNames(ms []CrashMod) string {
	var n []string
	for _, m := range ms {
		n = append(n, m.Name)
	}
	return strings.Join(n, ", ")
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func looksLikeFailure(log string) bool {
	low := strings.ToLower(log)
	for _, k := range []string{"incompatible mods found", "mod resolution failed", "missing or unsupported mandatory dependencies",
		"exception in thread \"main\"", "crash report saved", "game crashed", "outofmemoryerror", "unsupportedclassversionerror", "mixin apply failed"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// excerpt returns the most telling ~40 lines (around the first error).
func excerpt(lines []string) string {
	start := -1
	for i, l := range lines {
		low := strings.ToLower(l)
		if strings.Contains(low, "description:") || strings.Contains(low, "incompatible mods found") || strings.Contains(low, "exception") ||
			strings.Contains(low, "missing or unsupported") || strings.Contains(low, "error") {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := min(len(lines), start+40)
	var b strings.Builder
	sc := bufio.NewScanner(strings.NewReader(strings.Join(lines[start:end], "\n")))
	for sc.Scan() {
		b.WriteString(sc.Text())
		b.WriteByte('\n')
	}
	return b.String()
}
