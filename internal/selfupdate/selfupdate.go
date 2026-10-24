// Package selfupdate lets Kitties keep itself up to date. It looks at the releases of its
// repository on GitHub; when a newer one is out it either tells the person (notify) or, when the
// person chose auto, downloads the binary for this system, checks it against the release's
// SHA256SUMS, runs it once to see that it is the version it claims to be, puts it in place of the
// running one (keeping the old as <name>.prev) and restarts into it when nothing is in use.
//
// It only replaces a binary it is sure it may: a release build (not a development one), run from
// a folder it can write to, outside a container (those are updated with their image) and outside
// the Kitties app (whose bundle is signed). Everything else is told, with the way to update.
package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/config"
)

// Status is what is known about the version of Kitties.
type Status struct {
	Current string `json:"current"`
	// Latest is the newest release's tag, once looked up.
	Latest string `json:"latest,omitempty"`
	// Available is true when Latest is newer than Current.
	Available bool   `json:"available"`
	Mode      string `json:"mode"`
	// CanApply is true when this binary may be replaced and restarted by itself; when it is not,
	// Why says why and what to do instead.
	CanApply  bool      `json:"can_apply"`
	Why       string    `json:"why,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// ErrBusy is what an update gets while kitties are in use.
var ErrBusy = errors.New("the kitties are working or held: update when they are idle")

// maxBinary and maxSums bound what is downloaded.
const (
	maxBinary = 300 << 20
	maxSums   = 1 << 20
)

// Updater looks for new versions and applies them.
type Updater struct {
	cfg config.Updates
	// Current is the version of the running binary (what the linker put in main.version).
	Current string
	HTTP    *http.Client
	// Exe is the running binary; OS and Arch name the release asset for it.
	Exe, OS, Arch string
	InDocker      bool
	// Idle says whether nothing is in use (no task, no held desktop); nil means always.
	Idle func() bool
	// Restart replaces the running process with the binary at exe; tests replace it.
	Restart func(exe string, args, env []string) error
	// Now is the clock.
	Now func() time.Time

	mu     sync.Mutex
	status Status
	rel    release
	told   string
}

type release struct {
	Tag    string
	Assets map[string]string
}

// New makes the updater of the running binary.
func New(cfg config.Updates, current string) *Updater {
	exe, _ := os.Executable()
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	_, docker := os.Stat("/.dockerenv")
	u := &Updater{
		cfg: cfg, Current: current, Exe: exe, OS: runtime.GOOS, Arch: runtime.GOARCH,
		InDocker: docker == nil || os.Getenv("KITTIES_IN_DOCKER") != "",
		Restart:  restart, Now: time.Now,
		HTTP: &http.Client{Timeout: 10 * time.Minute},
	}
	u.HTTP.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if !secure(req.URL) {
			return errors.New("a download was sent to an address that is not https")
		}
		return nil
	}
	u.status = Status{Current: current, Mode: cfg.Gateway}
	return u
}

// secure says whether a download may come from this address: https, or this very machine (a test).
func secure(u *url.URL) bool {
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))
}

var semver = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?$`)

func parse(v string) (n [3]int, pre string, ok bool) {
	m := semver.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return n, "", false
	}
	for i := 0; i < 3; i++ {
		n[i], _ = strconv.Atoi(m[i+1])
	}
	return n, m[4], true
}

// isRelease says whether a version is a clean release tag (not "dev", and not a build some commits
// past one, like v1.2.3-4-gabcdef or v1.2.3-dirty).
func isRelease(v string) bool {
	_, pre, ok := parse(v)
	return ok && pre == ""
}

// Newer says whether the release tag is a newer version than the one running. Only a release can be
// moved on from, and only to another release.
func Newer(current, tag string) bool {
	c, _, ok := parse(current)
	if !ok || !isRelease(current) || !isRelease(tag) {
		return false
	}
	t, _, _ := parse(tag)
	for i := 0; i < 3; i++ {
		if t[i] != c[i] {
			return t[i] > c[i]
		}
	}
	return false
}

// assetName is the release file for this system.
func (u *Updater) assetName() string {
	name := "kitties-" + u.OS + "-" + u.Arch
	if u.OS == "windows" {
		name += ".exe"
	}
	return name
}

// canApply says whether this binary may be replaced by itself, and why not when it may not.
func (u *Updater) canApply() (bool, string) {
	switch {
	case !isRelease(u.Current):
		return false, "this is a development build: it does not update itself"
	case u.InDocker:
		return false, "this gateway runs in a container: update it with `docker compose pull && docker compose up -d`"
	case strings.Contains(filepath.ToSlash(u.Exe), ".app/Contents/") || strings.HasPrefix(filepath.Base(u.Exe), "kitties-gateway"):
		return false, "this gateway belongs to the Kitties app: update the app"
	case u.OS == "windows":
		return false, "a running program cannot be replaced on Windows: download the new version and replace it by hand"
	}
	probe, err := os.CreateTemp(filepath.Dir(u.Exe), ".kitties-update-*")
	if err != nil {
		return false, "the folder of this program cannot be written to by it: update it with your package manager or by hand"
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true, ""
}

// Status is what is known now, without asking GitHub.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.status
}

// Check asks GitHub for the newest release.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	can, why := u.canApply()
	st := Status{Current: u.Current, Mode: u.cfg.Gateway, CanApply: can, Why: why, CheckedAt: u.Now()}
	if !isRelease(u.Current) {
		// A development build has nothing to compare with: there is no use in asking.
		u.set(st, release{})
		return st, nil
	}
	rel, err := u.latest(ctx)
	if err != nil {
		st.Error = err.Error()
		u.set(st, release{})
		return st, err
	}
	st.Latest, st.Available = rel.Tag, Newer(u.Current, rel.Tag)
	u.set(st, rel)
	return st, nil
}

func (u *Updater) set(st Status, rel release) {
	u.mu.Lock()
	u.status, u.rel = st, rel
	u.mu.Unlock()
}

func (u *Updater) get(ctx context.Context, address string, limit int64, accept string) ([]byte, error) {
	parsed, err := url.Parse(address)
	if err != nil || !secure(parsed) {
		return nil, fmt.Errorf("%q is not an https address", address)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kitties/"+u.Current)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered HTTP %d", parsed.Host, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("the download is larger than expected")
	}
	return data, nil
}

func (u *Updater) latest(ctx context.Context) (release, error) {
	body, err := u.get(ctx, strings.TrimRight(u.cfg.API, "/")+"/repos/"+u.cfg.Repo+"/releases/latest", 4<<20, "application/vnd.github+json")
	if err != nil {
		return release{}, fmt.Errorf("looking for a new version: %w", err)
	}
	var doc struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Tag == "" {
		return release{}, errors.New("looking for a new version: the answer is not a release")
	}
	rel := release{Tag: doc.Tag, Assets: map[string]string{}}
	if doc.Prerelease || doc.Draft {
		rel.Tag = u.Current // a pre-release is nothing to move to
	}
	for _, a := range doc.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// Apply downloads the newest release for this system and puts it in place of the running binary.
// The caller restarts (Relaunch) when it has answered whoever asked.
func (u *Updater) Apply(ctx context.Context) (string, error) {
	st, err := u.Check(ctx)
	if err != nil {
		return "", err
	}
	switch {
	case !st.CanApply:
		return "", errors.New(st.Why)
	case !st.Available:
		return "", fmt.Errorf("%s is already the newest version", u.Current)
	}
	if u.Idle != nil && !u.Idle() {
		return "", ErrBusy
	}
	u.mu.Lock()
	rel := u.rel
	u.mu.Unlock()
	name := u.assetName()
	binURL, sumsURL := rel.Assets[name], rel.Assets["SHA256SUMS"]
	if binURL == "" || sumsURL == "" {
		return "", fmt.Errorf("the release %s has no %s with its SHA256SUMS", rel.Tag, name)
	}
	sums, err := u.get(ctx, sumsURL, maxSums, "")
	if err != nil {
		return "", fmt.Errorf("downloading SHA256SUMS: %w", err)
	}
	want := checksum(string(sums), name)
	if want == "" {
		return "", fmt.Errorf("SHA256SUMS of %s does not list %s", rel.Tag, name)
	}
	bin, err := u.get(ctx, binURL, maxBinary, "application/octet-stream")
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", name, err)
	}
	if sum := sha256.Sum256(bin); hex.EncodeToString(sum[:]) != want {
		return "", fmt.Errorf("%s does not match its SHA256SUMS: it was not installed", name)
	}
	if err := u.install(ctx, bin, rel.Tag); err != nil {
		return "", err
	}
	return rel.Tag, nil
}

// checksum finds a file's sha256 in the text of a SHA256SUMS file.
func checksum(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

// install checks the new binary runs and says the version it should, then swaps it in: the
// running one is kept as <name>.prev, so a bad update can be put back by hand.
func (u *Updater) install(ctx context.Context, bin []byte, tag string) error {
	dir := filepath.Dir(u.Exe)
	tmp, err := os.CreateTemp(dir, ".kitties-new-*")
	if err != nil {
		return fmt.Errorf("writing the new version: %w", err)
	}
	next := tmp.Name()
	defer os.Remove(next)
	if _, err := tmp.Write(bin); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing the new version: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(next, 0o755); err != nil {
		return err
	}
	run, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(run, next, "version").Output()
	if err != nil || !bytes.Contains(out, []byte(strings.TrimPrefix(tag, "v"))) {
		return fmt.Errorf("the new version did not run as %s (%v): it was not installed", tag, err)
	}
	prev := u.Exe + ".prev"
	_ = os.Remove(prev)
	if err := os.Rename(u.Exe, prev); err != nil {
		return fmt.Errorf("keeping the running version: %w", err)
	}
	if err := os.Rename(next, u.Exe); err != nil {
		_ = os.Rename(prev, u.Exe)
		return fmt.Errorf("putting the new version in place: %w", err)
	}
	return nil
}

// Relaunch restarts into the binary now in place, with the same arguments and environment. If it
// cannot, the program goes on running the old version and the status says so.
func (u *Updater) Relaunch() error {
	err := u.Restart(u.Exe, os.Args, os.Environ())
	if err != nil {
		u.mu.Lock()
		u.status.Error = "the new version is in place but Kitties could not restart into it: restart it by hand"
		u.mu.Unlock()
	}
	return err
}

// Hooks are what Watch tells the rest of the program.
type Hooks struct {
	// Window says whether the automatic updates may run now.
	Window func() bool
	// Available hears of a new version once (with Why, when it cannot be applied by itself).
	Available func(Status)
	// Updated hears that the new version is in place, just before the restart.
	Updated func(from, to string)
	Log     func(format string, args ...any)
}

// Watch looks for a new version shortly after the start and then every `every`, until ctx ends. In
// notify mode it only tells; in auto mode it also updates, inside the window, when nothing is in use.
func (u *Updater) Watch(ctx context.Context, first, every time.Duration, h Hooks) {
	if u.cfg.Gateway == config.UpdateOff {
		return
	}
	wait := first
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		u.round(ctx, h)
	}
}

func (u *Updater) logf(h Hooks, format string, args ...any) {
	if h.Log != nil {
		h.Log(format, args...)
	}
}

func (u *Updater) round(ctx context.Context, h Hooks) {
	st, err := u.Check(ctx)
	if err != nil {
		u.logf(h, "update: %v", err)
		return
	}
	if !st.Available {
		return
	}
	tell := func() {
		if u.told != st.Latest && h.Available != nil {
			u.told = st.Latest
			h.Available(st)
		}
	}
	if u.cfg.Gateway != config.UpdateAuto || !st.CanApply {
		tell()
		return
	}
	if h.Window != nil && !h.Window() {
		return
	}
	to, err := u.Apply(ctx)
	switch {
	case errors.Is(err, ErrBusy):
		return // the next round
	case err != nil:
		u.logf(h, "update: %v", err)
		tell()
		return
	}
	if h.Updated != nil {
		h.Updated(u.Current, to)
	}
	u.logf(h, "update: Kitties %s is in place; restarting", to)
	if err := u.Relaunch(); err != nil {
		u.logf(h, "update: it could not restart: %v: restart it to run %s", err, to)
	}
}
