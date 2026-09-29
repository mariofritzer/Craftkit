package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

type AdaptRequest struct {
	SourceID      string   `json:"sourceId"` // instance whose mods/settings are taken over (optional)
	Address       string   `json:"address"`
	ServerName    string   `json:"serverName"`
	MCVersion     string   `json:"mcVersion"`
	Loader        string   `json:"loader"`
	LoaderVersion string   `json:"loaderVersion"`
	Name          string   `json:"name"`
	ServerMods    []string `json:"serverMods"` // mod ids reported by the server to install as well
	CopySaves     bool     `json:"copySaves"`  // also copy worlds (for version upgrades)
}

type AdaptResult struct {
	Instance *Instance `json:"instance"`
	Plan     *Plan     `json:"plan,omitempty"`
	Copied   []string  `json:"copied"`
}

// adaptToServer creates an instance matching the server's version and brings the mods along.
func adaptToServer(j *Job, req AdaptRequest) (*AdaptResult, error) {
	var src *Instance
	if req.SourceID != "" {
		s, err := loadInstance(req.SourceID)
		if err != nil {
			return nil, err
		}
		src = s
	}
	if req.Loader == "" {
		if src != nil {
			req.Loader = src.Loader
		} else {
			req.Loader = "vanilla"
		}
	}
	if req.MCVersion == "" {
		return nil, errf("keine Minecraft-Version angegeben")
	}
	if req.Loader != "vanilla" && req.LoaderVersion == "" {
		j.setStep(sprintf("Suche passende %s-Version …", loaderNames[req.Loader]), -1)
		lvs, err := loaderVersionsFor(req.Loader, req.MCVersion)
		if err != nil {
			return nil, err
		}
		for _, v := range lvs {
			if v.Recommended {
				req.LoaderVersion = v.Version
			}
		}
		if req.LoaderVersion == "" {
			return nil, errf("%s gibt es nicht für Minecraft %s", loaderNames[req.Loader], req.MCVersion)
		}
	}
	if strings.TrimSpace(req.Name) == "" {
		base := loaderNames[req.Loader]
		if src != nil {
			base = src.Name
		}
		if req.ServerName != "" {
			req.Name = sprintf("%s – %s", req.ServerName, req.MCVersion)
		} else {
			req.Name = sprintf("%s – %s", base, req.MCVersion)
		}
	}
	mem := 0
	if src != nil {
		mem = src.MemoryGB
	}
	in, err := createInstance(j, CreateInstanceReq{Name: req.Name, MCVersion: req.MCVersion, Loader: req.Loader, LoaderVersion: req.LoaderVersion, MemoryGB: mem})
	if err != nil {
		return nil, err
	}
	res := &AdaptResult{Instance: in}

	if src != nil {
		j.setStep("Übernehme Einstellungen …", -1)
		names := []string{"options.txt", "optionsof.txt", "optionsshaders.txt", "servers.dat", "config", "resourcepacks", "shaderpacks", "schematics"}
		if req.CopySaves {
			names = append(names, "saves")
			j.logf("Kopiere Welten (die Originale bleiben unverändert) …")
		}
		for _, name := range names {
			from := filepath.Join(src.Dir, name)
			if !fileExists(from) {
				continue
			}
			if err := copyAll(from, filepath.Join(in.Dir, name)); err != nil {
				j.logf("Konnte %s nicht kopieren: %v", name, err)
				continue
			}
			res.Copied = append(res.Copied, name)
		}
		if len(res.Copied) > 0 {
			j.logf("Übernommen: %s", strings.Join(res.Copied, ", "))
		}
	}
	if req.Address != "" {
		instMu.Lock()
		in.ServerAddress = req.Address
		in.save()
		instMu.Unlock()
		sn := req.ServerName
		if sn == "" {
			sn = req.Address
		}
		if added, err := addServerToList(in.Dir, sn, req.Address); err != nil {
			j.logf("Server konnte nicht in die Mehrspieler-Liste eingetragen werden: %v", err)
		} else if added {
			j.logf("Server %s in die Mehrspieler-Liste eingetragen.", req.Address)
		}
	}

	// mods for the new version
	if in.Loader == "vanilla" {
		return res, nil
	}
	var reqs []PlanRequest
	var unknownFiles []string
	if src != nil {
		var skipped []string
		for _, it := range src.Items {
			if it.Explicit && !it.Disabled {
				reqs = append(reqs, PlanRequest{Source: it.Source, ProjectID: it.ProjectID})
			} else if it.Explicit {
				skipped = append(skipped, it.Name)
			}
		}
		defer func() {
			if res.Plan != nil && len(skipped) > 0 {
				res.Plan.Warnings = append(res.Plan.Warnings, sprintf("Deaktiviert und daher nicht übernommen: %s.", strings.Join(skipped, ", ")))
			}
		}()
		for _, f := range foreignFiles(filepath.Join(src.Dir, "mods"), src.Items) {
			unknownFiles = append(unknownFiles, f)
		}
	}
	var unresolved []string
	if len(req.ServerMods) > 0 {
		j.setStep("Suche die Mods des Servers …", -1)
		more, miss := serverModRequests(req.ServerMods)
		reqs = append(reqs, more...)
		unresolved = miss
	}
	if len(reqs) == 0 && len(unresolved) == 0 && len(unknownFiles) == 0 {
		return res, nil
	}
	j.setStep(sprintf("Suche passende Versionen für Minecraft %s …", in.MCVersion), -1)
	t, err := loadTarget("instance", in.ID)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Target: t}
	if len(reqs) > 0 {
		plan = newResolver().resolve(t, dedupeRequests(reqs))
	}
	for _, u := range unresolved {
		plan.Warnings = append(plan.Warnings, sprintf("Server-Mod „%s“ wurde nicht automatisch gefunden – bitte von Hand suchen.", u))
	}
	for _, f := range unknownFiles {
		plan.Warnings = append(plan.Warnings, sprintf("„%s“ ist nicht über CraftKit installiert und wurde nicht übernommen (erst „Online erkennen“ nutzen).", f))
	}
	storePlan(plan)
	res.Plan = plan
	return res, nil
}

func dedupeRequests(in []PlanRequest) []PlanRequest {
	seen := map[string]bool{}
	var out []PlanRequest
	for _, r := range in {
		k := itemKey(r.Source, r.ProjectID)
		if !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out
}

// serverModRequests maps mod ids reported by a server to Modrinth projects.
func serverModRequests(ids []string) (reqs []PlanRequest, unresolved []string) {
	var ms []*MissingDep
	for _, id := range ids {
		ms = append(ms, &MissingDep{ModID: id})
	}
	suggestForMissing(ms)
	for _, m := range ms {
		if m.ProjectID != "" {
			reqs = append(reqs, PlanRequest{Source: m.Source, ProjectID: m.ProjectID})
		} else {
			unresolved = append(unresolved, m.ModID)
		}
	}
	return
}

// linkServer remembers the server for an instance and adds it to the multiplayer list.
func linkServer(id, address, name string) (*Instance, bool, error) {
	instMu.Lock()
	defer instMu.Unlock()
	in, err := loadInstance(id)
	if err != nil {
		return nil, false, err
	}
	in.ServerAddress = strings.TrimSpace(address)
	if err := in.save(); err != nil {
		return nil, false, err
	}
	if in.ServerAddress == "" {
		return in, false, nil
	}
	if name == "" {
		name = address
	}
	added, err := addServerToList(in.Dir, name, address)
	return in, added, err
}

func copyAll(from, to string) error {
	st, err := os.Stat(from)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return copyFile(from, to)
	}
	return filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if fileExists(dst) {
			return nil
		}
		return copyFile(p, dst)
	})
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
