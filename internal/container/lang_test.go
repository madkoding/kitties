package container

import (
	"strings"
	"testing"
)

func TestLangTag(t *testing.T) {
	for in, want := range map[string]string{
		"es_CL.UTF-8": "es-CL", "es_CL@euro": "es-CL", "pt-br": "pt-BR", "en": "en", "es-419": "es-419",
		"zh-Hant-TW": "zh-Hant-TW", "C.UTF-8": "", "POSIX": "", "": "", "x y": "", "es_CL\n": "es-CL",
	} {
		if got := LangTag(in); got != want {
			t.Errorf("LangTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHostLanguage(t *testing.T) {
	env := map[string]string{}
	oldEnv, oldWin, oldMac := getEnv, hostUILanguages, appleLocale
	defer func() { getEnv, hostUILanguages, appleLocale = oldEnv, oldWin, oldMac }()
	getEnv = func(k string) string { return env[k] }
	hostUILanguages = func() []string { return []string{"es-CL", "en-US"} }
	appleLocale = func() string { return "fr_FR" }
	if got := HostLanguage(); got != "es-CL" {
		t.Errorf("windows = %q", got)
	}
	hostUILanguages = func() []string { return nil }
	if got := HostLanguage(); got != "fr-FR" {
		t.Errorf("mac = %q", got)
	}
	env["LANG"] = "C.UTF-8"
	if got := HostLanguage(); got != "fr-FR" {
		t.Errorf("C is no language: %q", got)
	}
	env["LANG"], env["LC_ALL"] = "de_DE.UTF-8", "pt_BR.UTF-8"
	if got := HostLanguage(); got != "pt-BR" {
		t.Errorf("LC_ALL first = %q", got)
	}
	if got := AcceptLanguages("es-CL"); got != "es-CL,es,en-US,en" {
		t.Errorf("accept = %q", got)
	}
	if got := AcceptLanguages("en-GB"); got != "en-GB,en" {
		t.Errorf("accept en = %q", got)
	}
	// The spec gets it, unless it sets its own.
	env = map[string]string{"LANG": "es_CL.UTF-8"}
	s := Spec{Name: "kitty-a", KittyName: "a", Image: "img"}
	if got := strings.Join(s.Args(), " "); !strings.Contains(got, "-e KITTY_LANG=es-CL") {
		t.Errorf("args = %s", got)
	}
	s.Env = map[string]string{LangEnv: "en-GB"}
	if got := strings.Join(s.Args(), " "); strings.Contains(got, "es-CL") || !strings.Contains(got, "KITTY_LANG=en-GB") {
		t.Errorf("own = %s", got)
	}
}
