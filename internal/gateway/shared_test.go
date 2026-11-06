package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/shared"
)

// archiveMaintainer is a maintainer that also exports and imports a kitty's files.
type archiveMaintainer struct {
	fakeMaintainer
	got       string
	importErr error
}

func (a *archiveMaintainer) Export(ref string, w io.Writer) error {
	_, err := w.Write([]byte("tgz-of-" + ref))
	return err
}
func (a *archiveMaintainer) Import(_ context.Context, ref string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	a.got = ref + ":" + string(b)
	return a.importErr
}

func sharedSetup(t *testing.T) (*httptest.Server, string, *artifacts.Store, *archiveMaintainer) {
	t.Helper()
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{
		"a1": {ID: "a1", Name: "ana", Status: kitty.StatusRunning, Desktop: true},
		"b1": {ID: "b1", Name: "bo", Status: kitty.StatusRunning},
	}}
	root := t.TempDir()
	write := func(p, text string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "ana", "report.md"), "# report")
	write(filepath.Join(root, "ana", "pics", "page.html"), "<script>alert(1)</script>")
	write(filepath.Join(root, "bo", "data.csv"), "1,2")
	write(filepath.Join(root, "ana", "big.bin"), strings.Repeat("x", 9<<20))
	arts := artifacts.New(t.TempDir())
	m := &archiveMaintainer{}
	c := &fakeChats{sessions: map[string]*chat.Session{}}
	srv := httptest.NewServer(New(f, c, "tok", "test").WithLibrary(nil, arts).WithMaintenance(m).WithShared(shared.New(root), 1024))
	t.Cleanup(srv.Close)
	return srv, root, arts, m
}

func TestSharedRoutes(t *testing.T) {
	srv, root, arts, _ := sharedSetup(t)
	code, got, _ := call(t, srv, "GET", "/v1/shared", "tok", nil)
	files, _ := got["files"].([]any)
	if code != 200 || len(files) != 4 || got["quota_mb"] != 1024.0 {
		t.Fatalf("list = %d %v", code, got)
	}
	if code, _, body := call(t, srv, "GET", "/v1/kitties/ana/shared/report.md", "tok", nil); code != 200 || string(body) != "# report" {
		t.Errorf("file = %d %q", code, body)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/v1/kitties/ana/shared/pics/page.html?download=1", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if h := resp.Header; h.Get("Content-Security-Policy") != "sandbox" || h.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(h.Get("Content-Disposition"), "attachment") {
		t.Errorf("a page is sandboxed and downloaded: %v", h)
	}
	for path, want := range map[string]int{
		"/v1/kitties/ana/shared/nope.md": 404, "/v1/kitties/ana/shared/.hidden": 400, "/v1/kitties/zz/shared/a.md": 404,
		"/v1/kitties/ana/shared/pics": 404,
	} {
		if code, _, _ := call(t, srv, "GET", path, "tok", nil); code != want {
			t.Errorf("GET %s = %d, want %d", path, code, want)
		}
	}
	// Kept in the library, as saved by its kitty; one too big for it is refused.
	if code, got, _ := call(t, srv, "POST", "/v1/kitties/ana/keep/report.md", "tok", nil); code != 200 || got["artifact"].(map[string]any)["by"] != "ana" {
		t.Errorf("keep = %d %v", code, got)
	}
	if b, _ := arts.Get("report.md"); string(b) != "# report" {
		t.Errorf("kept %q", b)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/keep/big.bin", "tok", nil); code != 413 && code != 400 {
		t.Errorf("too big for the library = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/keep/nope.md", "tok", nil); code != 404 {
		t.Errorf("keep a missing file = %d", code)
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/kitties/bo/shared/data.csv", "tok", nil); code != 204 {
		t.Errorf("delete = %d", code)
	}
	if _, err := os.Stat(filepath.Join(root, "bo", "data.csv")); !os.IsNotExist(err) {
		t.Error("not deleted")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/kitties/bo/shared/data.csv", "tok", nil); code != 404 {
		t.Errorf("delete again = %d", code)
	}
	noLib := httptest.NewServer(New(&fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana"}}},
		&fakeChats{sessions: map[string]*chat.Session{}}, "tok", "test").WithShared(shared.New(root), 0))
	defer noLib.Close()
	if code, _, _ := call(t, noLib, "POST", "/v1/kitties/ana/keep/report.md", "tok", nil); code != 404 {
		t.Errorf("no library = %d", code)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/shared", "", nil); code != 401 {
		t.Errorf("no token = %d", code)
	}
	if sharedCode(errors.New("x")) != 500 {
		t.Error("another failure")
	}
}

func TestSharedNeedsItsFolders(t *testing.T) {
	srv, _, _ := setup(t)
	for _, c := range [][2]string{{"GET", "/v1/shared"}, {"GET", "/v1/kitties/ana/shared/a.md"}, {"GET", "/v1/kitties/ana/export"}, {"POST", "/v1/kitties/ana/import"}} {
		if code, _, _ := call(t, srv, c[0], c[1], "tok", nil); code != 404 {
			t.Errorf("%s %s = %d", c[0], c[1], code)
		}
	}
}

func TestExportAndImport(t *testing.T) {
	srv, _, _, m := sharedSetup(t)
	req, _ := http.NewRequest("GET", srv.URL+"/v1/kitties/ana/export", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "tgz-of-a1" || !strings.Contains(resp.Header.Get("Content-Disposition"), "kitty-ana.tar.gz") {
		t.Errorf("export = %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/bo/export", "tok", nil); code != 400 {
		t.Errorf("no desktop = %d", code)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/zz/export", "tok", nil); code != 404 {
		t.Errorf("unknown = %d", code)
	}
	if code, got, _ := call(t, srv, "POST", "/v1/kitties/ana/import", "tok", "tgz"); code != 200 || got["imported"] != true || m.got != "ana:tgz" {
		t.Errorf("import = %d %v %q", code, got, m.got)
	}
	m.importErr = errors.New("start the desktop first: the files go in through it")
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/import", "tok", "tgz"); code != 409 {
		t.Errorf("stopped = %d", code)
	}
	m.importErr = errors.New("boom")
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/import", "tok", "tgz"); code != 500 {
		t.Errorf("failed = %d", code)
	}
	m.importErr = nil
	old := maxImport
	maxImport = 2
	t.Cleanup(func() { maxImport = old })
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/import", "tok", "toolong"); code != 413 {
		t.Errorf("too big = %d", code)
	}
}
