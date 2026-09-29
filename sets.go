package main

// Curated mod sets and the shader loader check.

import (
	"path/filepath"
	"strings"
	"time"
)

type ModSet struct {
	ID          string   `json:"id"`
	Icon        string   `json:"icon"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Slugs       []string `json:"slugs"` // Modrinth slugs; missing ones are skipped
}

var modSets = []ModSet{
	{"fps", "⚡", "Mehr FPS", "Schnelleres Rendering, weniger Arbeitsspeicher, kürzere Ladezeiten.",
		[]string{"sodium", "embeddium", "lithium", "ferrite-core", "entityculling", "immediatelyfast", "modernfix", "dynamic-fps"}},
	{"comfort", "🧭", "Komfort", "Rezeptanzeige, Info beim Anschauen von Blöcken, Hunger-Anzeige, bessere Steuerung.",
		[]string{"emi", "jade", "appleskin", "mouse-tweaks", "controlling", "modmenu"}},
	{"maps", "🗺️", "Karten", "Minikarte am Bildschirmrand und eine große Weltkarte.",
		[]string{"xaeros-minimap", "xaeros-world-map"}},
	{"shaders", "✨", "Shader-Grundlage", "Alles, was für Shader nötig ist – danach unter „Shader“ ein Shaderpack wählen.",
		[]string{"iris", "oculus"}},
}

// translatedSets returns the mod sets in the current UI language.
func translatedSets() []ModSet {
	out := make([]ModSet, len(modSets))
	for i, s := range modSets {
		s.Name, s.Description = L(s.Name), L(s.Description)
		out[i] = s
	}
	return out
}

// resolveSlugs maps Modrinth slugs to project ids (one request).
func resolveSlugs(slugs []string) (map[string]string, error) {
	var list []struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	if err := getJSON(modrinthAPI+"/projects?ids="+urlQueryEscape(mustJSON(slugs)), nil, time.Hour, &list); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, p := range list {
		out[p.Slug] = p.ID
	}
	return out, nil
}

func modSetRequests(id string) ([]PlanRequest, error) {
	for _, s := range modSets {
		if s.ID != id {
			continue
		}
		ids, err := resolveSlugs(s.Slugs)
		if err != nil {
			return nil, err
		}
		var reqs []PlanRequest
		for _, slug := range s.Slugs {
			if pid, ok := ids[slug]; ok {
				reqs = append(reqs, PlanRequest{Source: "modrinth", ProjectID: pid, Optional: true})
			}
		}
		return reqs, nil
	}
	return nil, errNew("unbekanntes Mod-Set")
}

// ShaderHelp tells the UI whether shaders can run and which mod would enable them.
type ShaderHelp struct {
	Supported bool   `json:"supported"`
	Via       string `json:"via,omitempty"` // detected shader mod
	Suggest   string `json:"suggest,omitempty"`
	SuggestID string `json:"suggestId,omitempty"`
	Message   string `json:"message,omitempty"`
}

func shaderHelp(in *Instance) *ShaderHelp {
	h := &ShaderHelp{}
	for _, ji := range scanFolder(filepath.Join(in.Dir, "mods")) {
		if ji.Disabled {
			continue
		}
		for _, p := range append([]string{ji.ModID}, ji.Provides...) {
			switch strings.ToLower(p) {
			case "iris", "oculus", "optifine", "optifabric":
				h.Supported, h.Via = true, ji.Name
				return h
			}
		}
	}
	switch in.Loader {
	case "vanilla":
		h.Message = L("Shader brauchen einen Mod-Loader. Lege eine Instanz mit Fabric oder NeoForge an und installiere dort Iris.")
		return h
	case "forge":
		h.Suggest = "oculus"
	default:
		h.Suggest = "iris"
	}
	if ids, err := resolveSlugs([]string{h.Suggest}); err == nil {
		h.SuggestID = ids[h.Suggest]
	}
	name := map[string]string{"iris": "Iris", "oculus": "Oculus"}[h.Suggest]
	h.Message = sprintf("Für Shader brauchst du die Mod %s – ohne sie werden Shaderpacks nicht geladen.", name)
	return h
}
