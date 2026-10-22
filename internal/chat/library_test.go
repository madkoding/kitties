package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/skills"
)

// fileConn answers with a file, like the diagram renderer.
type fileConn struct{ fail bool }

func (fileConn) Describe() string { return "draws" }
func (f fileConn) Call(context.Context, map[string]any) (string, error) {
	return "drawn", nil
}
func (f fileConn) CallFile(context.Context, map[string]any) (string, string, []byte, error) {
	if f.fail {
		return "saved as ../bad", "../bad", []byte("x"), nil
	}
	return "saved as d.svg", "d.svg", []byte("<svg/>"), nil
}

func TestParseDirectives(t *testing.T) {
	ds, rest := parseDirectives("Sure.\n@skill read browse-a-site\n`@artifact save {\"name\": \"a.md\", \"content\": \"hi\"}`\n@artifact list\n@skill save plain words\nbye")
	if len(ds) != 4 || rest != "Sure.\nbye" {
		t.Fatalf("parseDirectives = %+v %q", ds, rest)
	}
	if ds[0].Kind != "skill" || ds[0].Verb != "read" || ds[0].Args["name"] != "browse-a-site" {
		t.Errorf("read = %+v", ds[0])
	}
	if ds[1].Kind != "artifact" || ds[1].Verb != "save" || ds[1].Args["content"] != "hi" {
		t.Errorf("save = %+v", ds[1])
	}
	if ds[2].Verb != "list" || len(ds[2].Args) != 0 || ds[3].Args["name"] != "plain words" {
		t.Errorf("list/save = %+v %+v", ds[2], ds[3])
	}
}

func TestLibraryInChat(t *testing.T) {
	f, sa, _, _ := fleet(t)
	s := service(t, f)
	dir := t.TempDir()
	sk := skills.New(filepath.Join(dir, "skills"))
	arts := artifacts.New(filepath.Join(dir, "artifacts"))
	s.SetLibrary(sk, arts)
	sess, _ := s.Create("", []string{"bo"})
	sb := f.brains["b1"].(*scripted)
	sb.replies = []string{
		"Let me check.\n@skill read browse-a-site\n@artifact list\n@skill list browse",
		"Saving.\n@artifact save {\"name\": \"plan.md\", \"content\": \"# Plan\\n1. go\"}\n@skill save {\"name\": \"Launch Day\", \"body\": \"# Launch day\\n\\nWhat to do.\"}",
		"Reading back.\n@artifact read plan.md\n@artifact read missing.md\n@skill read nope\n@skill fly\n@artifact save {\"name\": \"../x\"}",
		"Done.",
	}
	replies, err := s.Send(context.Background(), sess.ID, "plan the launch")
	if err != nil {
		t.Fatal(err)
	}
	turn := replies[0]
	if turn.Text != "Let me check.\n\nSaving.\n\nReading back.\n\nDone." {
		t.Errorf("text = %q", turn.Text)
	}
	if strings.Join(turn.Skills, ",") != "browse-a-site,launch-day,launch-day" && strings.Join(turn.Skills, ",") != "browse-a-site,launch-day" {
		t.Errorf("skills = %v", turn.Skills)
	}
	if len(turn.Artifacts) != 1 || turn.Artifacts[0] != "plan.md" {
		t.Errorf("artifacts = %v", turn.Artifacts)
	}
	if !strings.Contains(sb.seen[0][0].Content, "Skills:\n- browse-a-site") || !strings.Contains(sb.seen[0][0].Content, "@artifact save") {
		t.Errorf("the prompt lists the library: %s", sb.seen[0][0].Content)
	}
	fb := func(i int) string { m := sb.seen[i]; return m[len(m)-1].Content }
	if !strings.Contains(fb(1), "[skill browse-a-site]") || !strings.Contains(fb(1), "# Browse a site") || !strings.Contains(fb(1), "(none yet)") || !strings.Contains(fb(1), "- browse-a-site: How to") {
		t.Errorf("round 1 feedback = %s", fb(1))
	}
	if !strings.Contains(fb(2), "[artifact plan.md] saved (text/markdown") || !strings.Contains(fb(2), "[skill launch-day] proposed") {
		t.Errorf("round 2 feedback = %s", fb(2))
	}
	if !strings.Contains(fb(3), "[artifact plan.md]") || !strings.Contains(fb(3), "# Plan") || !strings.Contains(fb(3), "failed: [artifact missing.md] no such artifact") || !strings.Contains(fb(3), "failed: [skill nope] no such skill") || !strings.Contains(fb(3), "unknown verb fly") || !strings.Contains(fb(3), "[artifact save] ") {
		t.Errorf("round 3 feedback = %s", fb(3))
	}
	list, _ := sk.List()
	found := false
	for _, x := range list {
		found = found || (x.Name == "launch-day" && x.Proposed)
	}
	if !found {
		t.Error("the proposal is in the library")
	}
	if idx := arts.Index(5); !strings.Contains(idx, "plan.md") || !strings.Contains(idx, "by bo") {
		t.Errorf("artifact index = %s", idx)
	}
	// A connector that answers with a file keeps it as an artifact.
	reg := connector.NewRegistry()
	reg.Add("draw", fileConn{})
	reg.Add("bad", fileConn{fail: true})
	f.conns = reg
	f.kitties["a1"].Connectors = nil
	sa.replies = []string{"@connector draw {\"source\": \"x\"}\n@connector bad {}", "There is your diagram."}
	ana, _ := s.Create("", []string{"ana"})
	replies, _ = s.Send(context.Background(), ana.ID, "draw it")
	turn = replies[0]
	if len(turn.Calls) != 2 || turn.Calls[0].Artifact != "d.svg" || turn.Calls[1].Err == "" || strings.Join(turn.Artifacts, ",") != "d.svg" {
		t.Errorf("file calls = %+v artifacts=%v", turn.Calls, turn.Artifacts)
	}
	if data, err := arts.Get("d.svg"); err != nil || string(data) != "<svg/>" {
		t.Error("the file is in the library")
	}
	// Without a library the directives say so.
	plain := service(t, f)
	sb.replies = []string{"@skill read x\n@artifact list", "ok"}
	sb.seen = nil
	p, _ := plain.Create("", []string{"bo"})
	if _, err := plain.Send(context.Background(), p.ID, "x"); err != nil {
		t.Fatal(err)
	}
	if len(sb.seen) != 2 {
		t.Fatalf("rounds = %d", len(sb.seen))
	}
	last := sb.seen[1]
	if !strings.Contains(last[len(last)-1].Content, "no skills library") || !strings.Contains(last[len(last)-1].Content, "no artifacts library") {
		t.Errorf("no library = %s", last[len(last)-1].Content)
	}
	if plain.libraryPrompt() != "" {
		t.Error("no library, no prompt")
	}
	// Directives in the same reply as desktop actions run before the task.
	d := &deskFleet{fakeFleet: f}
	desk := service(t, d)
	desk.SetLibrary(sk, arts)
	sa.replies = []string{"@artifact save {\"name\": \"notes.md\", \"content\": \"n\"}\n@tool done {\"summary\": \"fin\"}"}
	ds, _ := desk.Create("", []string{"ana"})
	replies, _ = desk.Send(context.Background(), ds.ID, "go")
	if len(replies[0].Artifacts) != 1 {
		t.Errorf("artifacts before the task: %+v", replies[0])
	}
	desk.Wait()
}
