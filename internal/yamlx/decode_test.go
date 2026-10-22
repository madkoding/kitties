package yamlx

import (
	"strings"
	"testing"
	"time"
)

type profile struct {
	Kind    string        `yaml:"kind"`
	Timeout time.Duration `yaml:"timeout"`
	Tags    []string      `yaml:"tags"`
	Ratio   float64       `yaml:"ratio"`
	On      bool          `yaml:"on"`
	Count   *int          `yaml:"count"`
}

type doc struct {
	Name     string             `yaml:"name"`
	Profiles map[string]profile `yaml:"profiles"`
	Labels   map[string]string  `yaml:"labels"`
	Ignored  string             `yaml:"-"`
	Hidden   string
}

func decodeDoc(t *testing.T, text string) (doc, error) {
	t.Helper()
	m, err := ParseYAML([]byte(text))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var d doc
	err = Decode(m, &d)
	return d, err
}

func TestDecodeMapOfBlocks(t *testing.T) {
	d, err := decodeDoc(t, "name: x\nprofiles:\n  fast:\n    kind: openai\n    timeout: 5s\n    tags: [a, b]\n    ratio: 0.5\n    on: true\n    count: 3\nlabels:\n  env: prod\n  n: 2\n")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	p := d.Profiles["fast"]
	if p.Kind != "openai" || p.Timeout != 5*time.Second || len(p.Tags) != 2 || p.Ratio != 0.5 || !p.On {
		t.Errorf("profile = %+v", p)
	}
	if p.Count == nil || *p.Count != 3 {
		t.Errorf("pointer field = %v", p.Count)
	}
	if d.Labels["env"] != "prod" || d.Labels["n"] != "2" {
		t.Errorf("labels = %v", d.Labels)
	}
}

func TestDecodeMapOfBlocksRejectsScalarEntry(t *testing.T) {
	_, err := decodeDoc(t, "profiles:\n  fast: text\n")
	if err == nil || !strings.Contains(err.Error(), "profiles.fast") {
		t.Errorf("error = %v", err)
	}
	_, err = decodeDoc(t, "profiles:\n  fast:\n    nope: 1\n")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown key inside a block entry must be named: %v", err)
	}
}

func TestDecodeRejectsUnsupportedMapAndBadInputs(t *testing.T) {
	type bad struct {
		M map[string]int `yaml:"m"`
	}
	m, _ := ParseYAML([]byte("m:\n  a: 1\n"))
	var b bad
	if err := Decode(m, &b); err == nil || !strings.Contains(err.Error(), "maps of blocks") {
		t.Errorf("error = %v", err)
	}
	type intKey struct {
		M map[int]string `yaml:"m"`
	}
	var ik intKey
	if err := Decode(m, &ik); err == nil {
		t.Error("a map keyed by int must be refused")
	}
	if err := Decode(m, nil); err == nil {
		t.Error("nil destination must be refused")
	}
	var notStruct int
	if err := Decode(m, &notStruct); err == nil {
		t.Error("a non-struct destination must be refused")
	}
}

func TestDecodeScalarShapes(t *testing.T) {
	cases := []struct {
		text string
		bad  string
	}{
		{"name: [a, b]\n", "list"},
		{"profiles:\n  p:\n    timeout: soon\n", "duration"},
		{"profiles:\n  p:\n    timeout: 1.5\n", ""},
		{"profiles:\n  p:\n    timeout: 90\n", ""},
		{"profiles:\n  p:\n    ratio: text\n", "number"},
		{"profiles:\n  p:\n    on: maybe\n", "true/false"},
		{"profiles:\n  p:\n    tags: text\n", "list"},
		{"profiles:\n  p:\n    count: 99999999999999999999\n", ""},
		{"profiles:\n  p:\n    count: text\n", ""},
		{"profiles:\n  p:\n    kind:\n", ""},
		{"profiles:\n  p:\n    ratio: 2\n", ""},
	}
	for _, c := range cases {
		_, err := decodeDoc(t, c.text)
		if c.bad == "" {
			if err != nil && (strings.Contains(c.text, "timeout") || strings.Contains(c.text, "kind") || strings.Contains(c.text, "ratio")) {
				t.Errorf("%q: unexpected error %v", c.text, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.bad) {
			t.Errorf("%q: error = %v, want %q", c.text, err, c.bad)
		}
	}
}
