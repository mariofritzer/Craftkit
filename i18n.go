package main

// Translations for texts produced by the backend (errors, progress, warnings).
// German is the source language; the other languages live in web/i18n/<lang>.json
// and are shared with the UI. Keys that contain %s/%d are Go format strings.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
)

var (
	langMu   sync.RWMutex
	curLang  = "de"
	langDict = map[string]map[string]string{}
)

// setLang switches the language used for backend texts (the UI sends it with every request).
func setLang(lang string) {
	if lang == "" || len(lang) > 10 {
		return
	}
	for _, c := range lang {
		if !(c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return
		}
	}
	langMu.Lock()
	curLang = lang
	langMu.Unlock()
}

func dictFor(lang string) map[string]string {
	langMu.RLock()
	d, ok := langDict[lang]
	langMu.RUnlock()
	if ok {
		return d
	}
	d = map[string]string{}
	if lang != "de" {
		if b, err := fs.ReadFile(webFS, "web/i18n/"+lang+".json"); err == nil {
			json.Unmarshal(b, &d)
		}
	}
	langMu.Lock()
	langDict[lang] = d
	langMu.Unlock()
	return d
}

// L translates a German text (or format string) into the current UI language.
func L(s string) string {
	langMu.RLock()
	lang := curLang
	langMu.RUnlock()
	if lang == "de" {
		return s
	}
	if t, ok := dictFor(lang)[s]; ok && t != "" {
		return t
	}
	return s
}

// errf is fmt.Errorf with a translated format string.
func errf(format string, a ...any) error { return fmt.Errorf(L(format), a...) }

// errNew is errors.New with a translated text.
func errNew(s string) error { return errors.New(L(s)) }

// sprintf is fmt.Sprintf with a translated format string.
func sprintf(format string, a ...any) string { return fmt.Sprintf(L(format), a...) }

// withLang picks up the language header of UI requests.
func withLang(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l := r.Header.Get("X-CraftKit-Lang"); l != "" {
			setLang(l)
		}
		h.ServeHTTP(w, r)
	})
}
