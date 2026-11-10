package container

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// THE HOST'S LANGUAGE.
//
// The desktop's browser asks for pages in the language of the host (Accept-Language, and its own
// menus), like its clock follows the host's zone: a Google or a bank that sees a browser in
// another language from the usual place takes it for a new device more often. The container gets
// it as KITTY_LANG (a BCP 47 tag such as es-CL), unless the spec sets its own.

// LangEnv is the variable the desktop's chromium wrapper reads.
const LangEnv = "KITTY_LANG"

var (
	// langTag matches what a locale name becomes: es, es-CL, zh-Hant-TW, pt-BR.
	langTag = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8}){0,2}$`)
	// hostUILanguages is the user's languages on Windows, first the preferred (es-CL); nil
	// elsewhere (lang_windows.go).
	hostUILanguages = func() []string { return nil }
	// appleLocale is macOS' locale (es_CL), empty elsewhere.
	appleLocale = func() string {
		if runtime.GOOS != "darwin" {
			return ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleLocale").Output()
		if err != nil {
			return ""
		}
		return string(out)
	}
)

// LangTag turns a locale name (es_CL.UTF-8, es_CL@euro, es-419, en) into a BCP 47 tag, or "" for
// none (C, POSIX, empty, nonsense).
func LangTag(locale string) string {
	l := strings.TrimSpace(locale)
	if i := strings.IndexAny(l, ".@"); i >= 0 {
		l = l[:i]
	}
	l = strings.ReplaceAll(l, "_", "-")
	if l == "" || l == "C" || l == "POSIX" {
		return ""
	}
	parts := strings.Split(l, "-")
	parts[0] = strings.ToLower(parts[0])
	if len(parts) > 1 && len(parts[len(parts)-1]) == 2 {
		parts[len(parts)-1] = strings.ToUpper(parts[len(parts)-1])
	}
	l = strings.Join(parts, "-")
	if !langTag.MatchString(l) {
		return ""
	}
	return l
}

// HostLanguage is the host's language as a BCP 47 tag: $LC_ALL, $LC_MESSAGES, $LANG, Windows'
// preferred language or macOS' locale; "" when none tells it.
func HostLanguage() string {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l := LangTag(getEnv(v)); l != "" {
			return l
		}
	}
	for _, l := range hostUILanguages() {
		if t := LangTag(l); t != "" {
			return t
		}
	}
	return LangTag(appleLocale())
}

// AcceptLanguages is what the browser asks for with a tag: the tag, its language alone, then
// English as the last resort (es-CL → es-CL,es,en-US,en).
func AcceptLanguages(tag string) string {
	if tag == "" {
		return ""
	}
	out := []string{tag}
	base, _, _ := strings.Cut(tag, "-")
	if base != tag {
		out = append(out, base)
	}
	if base != "en" {
		out = append(out, "en-US", "en")
	}
	return strings.Join(out, ",")
}
