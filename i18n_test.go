package main

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestTranslations(t *testing.T) {
	files, _ := fs.Glob(webFS, "web/i18n/*.json")
	if len(files) < 13 {
		t.Fatalf("only %d translation files", len(files))
	}
	verbs := regexp.MustCompile(`%[-+#0-9.]*[a-zA-Z%]`)
	for _, f := range files {
		b, _ := fs.ReadFile(webFS, f)
		var d map[string]string
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for k, v := range d {
			if strings.Join(verbs.FindAllString(k, -1), " ") != strings.Join(verbs.FindAllString(v, -1), " ") {
				t.Errorf("%s: format verbs differ: %q -> %q", f, k, v)
			}
		}
	}
	defer setLang("de")
	setLang("en")
	if got := errf("Instanz %q nicht gefunden", "x").Error(); got == `Instanz "x" nicht gefunden` {
		t.Errorf("not translated: %s", got)
	}
	setLang("../../etc")
	if L("Fertig.") == "Fertig." {
		t.Errorf("invalid language code must be ignored")
	}
	setLang("de")
	if L("Fertig.") != "Fertig." {
		t.Errorf("German must stay unchanged")
	}
}
