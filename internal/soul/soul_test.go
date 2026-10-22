package soul

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultIsHelpfulKindAdaptable(t *testing.T) {
	s := Default("Mimi")
	p := s.SystemPrompt(Context{})
	for _, want := range []string{"You are Mimi", "helpful", "kind", "adaptable", "language the person writes in"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
}

func TestSystemPromptRendersEveryPart(t *testing.T) {
	s := Soul{Persona: "a tester", Tone: "dry", Traits: []string{"careful"}, Skills: []string{"go"},
		Language: "Spanish", Instructions: "  Never guess.  "}
	p := s.SystemPrompt(Context{KittyName: "Tom", Desktop: true, Workspace: "/work", Group: []string{"Ana", "Bo"},
		Connectors: map[string]string{"slack": "posts to Slack", "mail": "sends mail"}, User: "Pat"})
	for _, want := range []string{"You are Tom", "You are a tester.", "Your tone is dry.", "careful", "good at: go",
		"answer in Spanish", "kind to Pat", "virtual X display", "/work", "Ana, Bo", "@name", "- mail: sends mail\n- slack: posts to Slack", "Never guess."} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if !strings.Contains(Soul{}.SystemPrompt(Context{}), "You are Kitty") {
		t.Error("a nameless soul is called Kitty")
	}
}

func TestLoadParseMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "soul.yaml")
	if err := os.WriteFile(path, []byte("name: Luna\npersona: a planner\ntraits: [calm]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil || s.Name != "Luna" || s.Persona != "a planner" || len(s.Traits) != 1 {
		t.Fatalf("Load = %+v, %v", s, err)
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("a missing file must fail")
	}
	if _, err := Parse([]byte("name: [\n")); err == nil {
		t.Error("bad YAML must fail")
	}
	if _, err := Parse([]byte("mood: happy\n")); err == nil {
		t.Error("an unknown field must fail")
	}
	m := Merge(Default("x"), Soul{Name: "Y", Persona: "p", Tone: "t", Traits: []string{"a"}, Instructions: "i", Language: "l", Skills: []string{"s"}})
	if m.Name != "Y" || m.Persona != "p" || m.Tone != "t" || m.Traits[0] != "a" || m.Instructions != "i" || m.Language != "l" || m.Skills[0] != "s" {
		t.Errorf("Merge = %+v", m)
	}
	keep := Merge(Default("x"), Soul{})
	if keep.Name != "x" || len(keep.Traits) != 4 {
		t.Errorf("an empty override keeps the base: %+v", keep)
	}
}
