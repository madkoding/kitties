package orchestrator

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/container"
	"github.com/madkoding/kitties/internal/kitty"
)

// homeFake is a runtime that keeps homes on the host: what it is asked is remembered.
type homeFake struct {
	*fakeContainers
	label    bool            // the image seeds a home of the host
	mounted  map[string]bool // containers with their home mounted
	hasErr   error
	outErr   error
	inErr    error
	inOut    string
	restErr  error
	imported []string // what each ExecIn read, with its command
	specs    []container.Spec
}

func newHomeFake() *homeFake {
	return &homeFake{fakeContainers: newFake(), label: true, mounted: map[string]bool{}}
}

func (h *homeFake) Up(ctx context.Context, s container.Spec) (string, error) {
	h.specs = append(h.specs, s)
	id, err := h.fakeContainers.Up(ctx, s)
	if err == nil && s.Home != "" {
		h.mounted[s.Name] = true
	}
	return id, err
}
func (h *homeFake) Remove(ctx context.Context, n string) error {
	delete(h.mounted, n)
	return h.fakeContainers.Remove(ctx, n)
}
func (h *homeFake) ImageHasHome(context.Context, string) (bool, error) { return h.label, nil }
func (h *homeFake) HasHome(_ context.Context, n string) (bool, error) {
	return h.mounted[n], h.hasErr
}
func (h *homeFake) ExecOut(_ context.Context, n string, w io.Writer, argv ...string) error {
	h.calls = append(h.calls, "out "+strings.Join(argv, " "))
	if h.outErr != nil {
		_, _ = w.Write([]byte("partial"))
		return h.outErr
	}
	_, err := w.Write([]byte("home-of-" + n))
	return err
}
func (h *homeFake) ExecIn(_ context.Context, n string, r io.Reader, argv ...string) (string, error) {
	b, _ := io.ReadAll(r)
	h.imported = append(h.imported, strings.Join(argv, " ")+"<"+string(b))
	return h.inOut, h.inErr
}
func (h *homeFake) Restart(_ context.Context, n string) error {
	h.calls = append(h.calls, "restart")
	return h.restErr
}

func homeManager(t *testing.T) (*Manager, *homeFake) {
	t.Helper()
	h := newHomeFake()
	m, err := New(testConfig(t), WithContainers(h, nil))
	if err != nil {
		t.Fatal(err)
	}
	return m, h
}

func TestANewDesktopGetsAHomeAndSharedOfTheHost(t *testing.T) {
	ctx := context.Background()
	m, h := homeManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	s := h.specs[0]
	data := m.cfg.DataDirOr()
	if s.Home != filepath.Join(data, "homes", "mimi") || s.Shared != filepath.Join(data, "shared", "mimi") ||
		s.Env["KITTY_SHARED_QUOTA_MB"] != "5120" {
		t.Errorf("spec = %+v", s)
	}
	for _, dir := range []string{s.Home, s.Shared} {
		st, err := os.Stat(dir)
		if err != nil || st.Mode().Perm() != 0o777 {
			t.Errorf("%s: %v %v", dir, st, err)
		}
		if root, _ := os.Stat(filepath.Dir(dir)); root.Mode().Perm() != 0o700 {
			t.Errorf("the root of %s is open", dir)
		}
	}
	// An image without the label keeps its own home; the Shared folder is always there.
	h.label = false
	m.cfg.Containers.SharedQuotaMB = -1
	_ = m.Stop(ctx, k.ID)
	h.stale = true
	m.cfg.Containers.AutoUpdate = true
	if err := m.Start(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if s := h.specs[len(h.specs)-1]; s.Home != "" || s.Shared == "" || s.Env["KITTY_SHARED_QUOTA_MB"] != "0" {
		t.Errorf("old image: %+v", s)
	}
	m.cfg.Containers.SharedQuotaMB = 50
	if m.SharedQuotaMB() != 50 {
		t.Error("a quota of its own")
	}
	if m.cfg.Containers.Env != nil && m.cfg.Containers.Env["KITTY_SHARED_QUOTA_MB"] != "" {
		t.Error("the configuration's environment was changed")
	}
}

func TestAnOlderDesktopsHomeMovesToTheHost(t *testing.T) {
	ctx := context.Background()
	m, h := homeManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	// A desktop made before: running, its home inside.
	h.exists[k.Container], h.running[k.Container] = true, true
	h.stale = true
	if err := m.renew(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(h.calls, "|")
	if !strings.Contains(calls, "out tar -C /home/kitty --exclude=./work --exclude=./Shared --exclude=./.cache -cf - .") ||
		strings.Index(calls, "out tar") > strings.Index(calls, "rm") || !strings.Contains(calls, "restart") {
		t.Errorf("calls = %q", calls)
	}
	if len(h.imported) != 1 || h.imported[0] != "kitty-import --home<home-of-"+k.Container {
		t.Errorf("imported = %q", h.imported)
	}
	if _, err := os.Stat(m.movePath(k)); !os.IsNotExist(err) {
		t.Error("the copy stays after it was put back")
	}
	// Now the home is on the host: nothing is copied again.
	h.calls, h.imported = nil, nil
	if err := m.renew(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(h.calls, "|"), "out tar") || len(h.imported) != 0 {
		t.Errorf("moved twice: %v %v", h.calls, h.imported)
	}
}

func TestAStoppedOlderDesktopIsStartedToCopyItsHome(t *testing.T) {
	ctx := context.Background()
	m, h := homeManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	h.exists[k.Container] = true
	h.stale = true
	m.cfg.Containers.AutoUpdate = true
	if err := m.Start(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(h.calls, "|")
	if !strings.HasPrefix(strings.SplitN(calls, "|out", 2)[0], "list|start") {
		t.Errorf("it is started before the copy: %q", calls)
	}
}

func TestAHomeThatCannotBeCopiedKeepsTheOldDesktop(t *testing.T) {
	ctx := context.Background()
	m, h := homeManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	h.exists[k.Container], h.running[k.Container] = true, true
	h.stale = true
	h.outErr = errors.New("tar: no space")
	err := m.renew(ctx, k, false)
	if err == nil || !strings.Contains(err.Error(), "left as it is") || !strings.Contains(err.Error(), "no space") {
		t.Errorf("renew = %v", err)
	}
	if !h.exists[k.Container] || strings.Contains(strings.Join(h.calls, "|"), "rm") {
		t.Error("the old desktop was removed")
	}
	if _, err := os.Stat(m.movePath(k) + ".part"); !os.IsNotExist(err) {
		t.Error("a partial copy stays")
	}
	got, _ := m.Get(k.ID)
	if got.Status != kitty.StatusError {
		t.Errorf("status = %s", got.Status)
	}
	// It cannot tell where the home is: kept too.
	h.outErr, h.hasErr = nil, errors.New("inspect failed")
	if err := m.renew(ctx, k, false); err == nil || !h.exists[k.Container] {
		t.Errorf("inspect fails: %v", err)
	}
	// A stopped one that cannot be started for the copy: kept.
	h.hasErr = nil
	h.running[k.Container] = false
	h.fail = "start"
	if err := m.renew(ctx, k, false); err == nil || !h.exists[k.Container] {
		t.Errorf("start fails: %v", err)
	}
	// The folder for the copy cannot be made.
	h.fail = ""
	h.running[k.Container] = true
	_ = os.RemoveAll(filepath.Join(m.cfg.DataDirOr(), "homes", ".moving"))
	if err := os.WriteFile(filepath.Join(m.cfg.DataDirOr(), "homes", ".moving"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.renew(ctx, k, false); err == nil || !h.exists[k.Container] {
		t.Errorf("no folder: %v", err)
	}
}

func TestAMoveThatFailsIsTriedAgainAtTheNextStart(t *testing.T) {
	ctx := context.Background()
	m, h := homeManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	h.exists[k.Container], h.running[k.Container] = true, true
	h.stale = true
	h.inErr = errors.New("kitty-import: not found")
	if err := m.renew(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.movePath(k)); err != nil {
		t.Fatal("the copy is gone after a failed import")
	}
	info, _ := m.DesktopInfo(ctx, k.ID)
	if !info.Storage.Moving || !info.Storage.Home {
		t.Errorf("storage = %+v", info.Storage)
	}
	// The restart fails: the copy stays as well.
	h.inErr, h.restErr = nil, errors.New("restart failed")
	if err := m.Start(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.movePath(k)); err != nil {
		t.Fatal("the copy is gone after a failed restart")
	}
	h.restErr = nil
	if err := m.Start(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.movePath(k)); !os.IsNotExist(err) {
		t.Error("the copy stays after it was put back")
	}
}

func TestARuntimeWithoutHomesWorksAsBefore(t *testing.T) {
	ctx := context.Background()
	m, f, _ := sessionsManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	_ = m.Start(ctx, k.ID)
	f.stale = true
	if err := m.renew(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	if err := m.Import(ctx, k.ID, strings.NewReader("x")); err == nil || !strings.Contains(err.Error(), "cannot take files") {
		t.Errorf("import = %v", err)
	}
	m.finishMove(ctx, k, container.Spec{Home: "/x"})
	if err := m.keepOldHome(ctx, k, true); err != nil {
		t.Error(err)
	}
}

func TestStorageTellsWhatIsKept(t *testing.T) {
	ctx := context.Background()
	m, _ := homeManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	_ = m.Start(ctx, k.ID)
	k, _ = m.Get(k.ID)
	write(t, filepath.Join(m.homeDir(k), ".config", "app", "a"), "12345")
	write(t, filepath.Join(k.Workspace, "w"), "123")
	write(t, filepath.Join(m.sharedDir(k), "s.png"), "1")
	info, err := m.DesktopInfo(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s := info.Storage; !s.Home || s.HomeBytes != 5 || s.WorkBytes != 3 || s.SharedBytes != 1 || s.SharedQuotaMB != 5120 || s.Moving {
		t.Errorf("storage = %+v", s)
	}
	old := sizeCap
	sizeCap = 1
	t.Cleanup(func() { sizeCap = old })
	if n := dirSize(m.homeDir(k)); n != 0 {
		t.Errorf("capped size = %d", n)
	}
	if dirSize(filepath.Join(m.homeDir(k), "nope")) != 0 {
		t.Error("a missing folder")
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExportLeavesTheSecretsOut(t *testing.T) {
	ctx := context.Background()
	m, _ := homeManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	_ = m.Start(ctx, k.ID)
	k, _ = m.Get(k.ID)
	home := m.homeDir(k)
	write(t, filepath.Join(home, ".config", "app", "settings"), "s")
	write(t, filepath.Join(home, ".local", "bin", "tool"), "t")
	write(t, filepath.Join(home, ".ssh", "id_ed25519"), "KEY")
	write(t, filepath.Join(home, ".git-credentials"), "TOKEN")
	write(t, filepath.Join(home, ".config", "gh", "hosts.yml"), "TOKEN")
	write(t, filepath.Join(home, ".cache", "big"), "c")
	write(t, filepath.Join(k.Workspace, "notes.md"), "n")
	write(t, filepath.Join(k.Workspace, ".browser", "Default", "Cookies"), "COOKIE")
	write(t, filepath.Join(m.sharedDir(k), "report.pdf"), "pdf")
	if err := os.Symlink("/etc/passwd", filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "secret-of-kitty"), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := m.Export(k.ID, &buf); err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		if strings.Contains(string(b), "KEY") || strings.Contains(string(b), "TOKEN") || strings.Contains(string(b), "COOKIE") {
			t.Errorf("a secret in %s", h.Name)
		}
		if h.Name == "home/link" && (h.Typeflag != tar.TypeSymlink || h.Linkname != "/etc/passwd") {
			t.Errorf("a link is kept as a link: %+v", h)
		}
		names = append(names, h.Name)
	}
	sort.Strings(names)
	got := strings.Join(names, " ")
	for _, want := range []string{"home/.config/app/settings", "home/.local/bin/tool", "home/link", "work/notes.md", "shared/report.pdf"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is missing: %s", want, got)
		}
	}
	for _, not := range []string{".ssh", ".git-credentials", "home/.config/gh", ".cache", ".browser", "home/work", "home/Shared"} {
		if strings.Contains(got, not) {
			t.Errorf("%s is in the export: %s", not, got)
		}
	}
	if os.Getuid() != 0 && strings.Contains(got, "secret-of-kitty") {
		t.Error("an unreadable file")
	}
	if err := m.Export("nobody", &buf); err == nil {
		t.Error("an unknown kitty")
	}
	if err := m.Export(k.ID, failWriter{}); err == nil {
		t.Error("a writer that fails")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestImportGoesThroughTheRunningDesktop(t *testing.T) {
	ctx := context.Background()
	m, h := homeManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	if err := m.Import(ctx, k.ID, strings.NewReader("tgz")); !errors.Is(err, errNotRunning) {
		t.Errorf("stopped: %v", err)
	}
	_ = m.Start(ctx, k.ID)
	if err := m.Import(ctx, k.ID, strings.NewReader("tgz")); err != nil || h.imported[len(h.imported)-1] != "kitty-import<tgz" {
		t.Errorf("import = %v %v", err, h.imported)
	}
	h.inErr, h.inOut = errors.New("exec: \"kitty-import\": executable file not found in $PATH"), ""
	if err := m.Import(ctx, k.ID, strings.NewReader("tgz")); err == nil || !strings.Contains(err.Error(), "update the desktop") {
		t.Errorf("old image: %v", err)
	}
	h.inErr = errors.New("tar: bad")
	if err := m.Import(ctx, k.ID, strings.NewReader("tgz")); err == nil || !strings.Contains(err.Error(), "tar: bad") {
		t.Errorf("bad tar: %v", err)
	}
	h.fail = "list"
	if err := m.Import(ctx, k.ID, strings.NewReader("tgz")); err == nil {
		t.Error("the runtime does not answer")
	}
	if err := m.Import(ctx, "nobody", nil); err == nil {
		t.Error("an unknown kitty")
	}
}

func TestDesktopSpecFailsWhenItsFoldersCannotBeMade(t *testing.T) {
	ctx := context.Background()
	m, h := homeManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	data := m.cfg.DataDirOr()
	write(t, filepath.Join(data, "shared"), "a file in the way")
	if err := m.Start(ctx, k.ID); err == nil || !strings.Contains(err.Error(), "Shared folder") {
		t.Errorf("shared: %v", err)
	}
	_ = os.Remove(filepath.Join(data, "shared"))
	write(t, filepath.Join(data, "homes"), "a file in the way")
	if err := m.Start(ctx, k.ID); err == nil || !strings.Contains(err.Error(), "home") {
		t.Errorf("home: %v", err)
	}
	_ = os.Remove(filepath.Join(data, "homes"))
	if err := makeOpenDir(filepath.Join(data, "x", "y")); err != nil {
		t.Error(err)
	}
	_ = h
}
