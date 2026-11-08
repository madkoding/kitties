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
	CanApply bool `json:"can_apply"`
	// Signed is true when releases are checked against a signing key; without one an update is
	// only checked against the release's checksums, which do not say who made it.
	Signed    bool      `json:"signed"`
	Why       string    `json:"why,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
	Error     string    `json:"error,omitempty"`
	// Skipped is a newer version that was tried here and did not hold, and is not offered again.
	Skipped string `json:"skipped,omitempty"`
	// URL is the page of the newest release; Changes are the lines of its "What's Changed".
	URL     string   `json:"url,omitempty"`
	Changes []string `json:"changes,omitempty"`
	// Progress is the update being made now (or the last one, when it failed).
	Progress *Progress `json:"progress,omitempty"`
}

// Progress is how far an update has gone: the step it is at, the bytes of the download, and the
// whole of it as a percentage.
type Progress struct {
	To string `json:"to"`
	// Phase is check, download, verify, install, restart, or failed.
	Phase   string `json:"phase"`
	Percent int    `json:"percent"`
	Done    int64  `json:"done,omitempty"`
	Total   int64  `json:"total,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Update phases.
const (
	PhaseCheck    = "check"
	PhaseDownload = "download"
	PhaseVerify   = "verify"
	PhaseInstall  = "install"
	PhaseRestart  = "restart"
	PhaseFailed   = "failed"
)

// ErrInProgress is what a second update gets while one is being made.
var ErrInProgress = errors.New("an update is already being made")

// applyTimeout bounds an update made in the background.
var applyTimeout = 15 * time.Minute

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

	mu       sync.Mutex
	status   Status
	rel      release
	told     string
	progress *Progress
	running  bool
}

type release struct {
	Tag     string
	URL     string
	Changes []string
	Assets  map[string]string
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
	case u.cfg.Gateway == config.UpdateAuto && u.publicKey() == "":
		return false, "updating by itself needs the key the releases are signed with: set updates.public_key (kitties release-key makes one)"
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
	st := u.status
	if u.progress != nil {
		p := *u.progress
		st.Progress = &p
	}
	return st
}

// step records how far the update has gone.
func (u *Updater) step(phase string, percent int, done, total int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.progress == nil {
		u.progress = &Progress{}
	}
	u.progress.Phase, u.progress.Percent, u.progress.Done, u.progress.Total, u.progress.Error = phase, percent, done, total, ""
}

// fail records that the update stopped, and why.
func (u *Updater) fail(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.progress == nil {
		u.progress = &Progress{}
	}
	u.progress.Phase, u.progress.Error = PhaseFailed, err.Error()
}

// Check asks GitHub for the newest release.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	can, why := u.canApply()
	st := Status{Current: u.Current, Mode: u.cfg.Gateway, CanApply: can, Why: why, CheckedAt: u.Now(), Signed: u.publicKey() != ""}
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
	if st.Available {
		st.URL, st.Changes = rel.URL, rel.Changes
	}
	// A version that did not hold on this installation is not offered again; a newer one is.
	if st.Latest == u.skipped() {
		st.Available = false
		st.Skipped = st.Latest
	}
	u.set(st, rel)
	return st, nil
}

func (u *Updater) set(st Status, rel release) {
	u.mu.Lock()
	u.status, u.rel = st, rel
	u.mu.Unlock()
}

func (u *Updater) get(ctx context.Context, address string, limit int64, accept string) ([]byte, error) {
	return u.fetch(ctx, address, limit, accept, nil)
}

// counter tells how many bytes have been read, and of how many, as they arrive.
type counter struct {
	r           io.Reader
	done, total int64
	tell        func(done, total int64)
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	c.tell(c.done, c.total)
	return n, err
}

func (u *Updater) fetch(ctx context.Context, address string, limit int64, accept string, tell func(done, total int64)) ([]byte, error) {
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
	var body io.Reader = resp.Body
	if tell != nil {
		body = &counter{r: resp.Body, total: resp.ContentLength, tell: tell}
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
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
		URL        string `json:"html_url"`
		Body       string `json:"body"`
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
	rel := release{Tag: doc.Tag, Assets: map[string]string{}, Changes: changes(doc.Body)}
	if u, err := url.Parse(doc.URL); err == nil && u.Scheme == "https" {
		rel.URL = doc.URL
	}
	if doc.Prerelease || doc.Draft {
		rel.Tag = u.Current // a pre-release is nothing to move to
	}
	for _, a := range doc.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// maxChanges bounds the lines of a release's changes that are told.
const maxChanges = 20

var byAuthor = regexp.MustCompile(`\s+by @[\w-]+(\s+in\s+https?://\S+)?\s*$`)

// changes reads the "* …" lines of a release's notes (GitHub's "What's Changed"), without the
// author and the link of each pull request.
func changes(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "* ") && !strings.HasPrefix(line, "- ") {
			continue
		}
		line = strings.TrimSpace(byAuthor.ReplaceAllString(line[2:], ""))
		if line == "" {
			continue
		}
		if len(line) > 200 {
			line = line[:200] + "…"
		}
		if out = append(out, line); len(out) == maxChanges {
			break
		}
	}
	return out
}

// ready checks that the newest release can be installed now, and returns it.
func (u *Updater) ready(ctx context.Context) (release, error) {
	st, err := u.Check(ctx)
	if err != nil {
		return release{}, err
	}
	switch {
	case !st.CanApply:
		return release{}, errors.New(st.Why)
	case !st.Available:
		return release{}, fmt.Errorf("%s is already the newest version", u.Current)
	}
	if u.Idle != nil && !u.Idle() {
		return release{}, ErrBusy
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.rel, nil
}

// Apply downloads the newest release for this system and puts it in place of the running binary.
// The caller restarts (Relaunch) when it has answered whoever asked.
func (u *Updater) Apply(ctx context.Context) (string, error) {
	if !u.claim() {
		return "", ErrInProgress
	}
	defer u.unclaim()
	rel, err := u.ready(ctx)
	if err != nil {
		return "", err
	}
	return u.install(ctx, rel)
}

// Begin checks that the update can be made now and makes it in the background, telling how far it
// has gone in the status (Progress); done hears how it ended. It answers the version it moves to,
// or why it does not.
func (u *Updater) Begin(ctx context.Context, done func(to string, err error)) (string, error) {
	if !u.claim() {
		return "", ErrInProgress
	}
	u.step(PhaseCheck, 2, 0, 0)
	rel, err := u.ready(ctx)
	if err != nil {
		u.mu.Lock()
		u.progress = nil
		u.mu.Unlock()
		u.unclaim()
		return "", err
	}
	u.mu.Lock()
	u.progress.To = rel.Tag
	u.mu.Unlock()
	go func() {
		defer u.unclaim()
		run, cancel := context.WithTimeout(context.Background(), applyTimeout)
		defer cancel()
		to, err := u.install(run, rel)
		if err != nil {
			u.fail(err)
		} else {
			u.step(PhaseRestart, 100, 0, 0)
		}
		if done != nil {
			done(to, err)
		}
	}()
	return rel.Tag, nil
}

func (u *Updater) claim() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.running {
		return false
	}
	u.running = true
	return true
}

func (u *Updater) unclaim() {
	u.mu.Lock()
	u.running = false
	u.mu.Unlock()
}

// install downloads the release's binary for this system, checks it, and puts it in place.
func (u *Updater) install(ctx context.Context, rel release) (string, error) {
	name := u.assetName()
	binURL, sumsURL := rel.Assets[name], rel.Assets["SHA256SUMS"]
	if binURL == "" || sumsURL == "" {
		return "", fmt.Errorf("the release %s has no %s with its SHA256SUMS", rel.Tag, name)
	}
	sums, err := u.get(ctx, sumsURL, maxSums, "")
	if err != nil {
		return "", fmt.Errorf("downloading SHA256SUMS: %w", err)
	}
	// With a key, the checksums must be signed by it: nothing is trusted from a release that is not.
	if key := u.publicKey(); key != "" {
		sigURL := rel.Assets["SHA256SUMS.sig"]
		if sigURL == "" {
			return "", fmt.Errorf("the release %s is not signed (it has no SHA256SUMS.sig): it was not installed", rel.Tag)
		}
		sig, err := u.get(ctx, sigURL, 4<<10, "")
		if err != nil {
			return "", fmt.Errorf("downloading SHA256SUMS.sig: %w", err)
		}
		if err := Verify(key, sums, string(sig)); err != nil {
			return "", fmt.Errorf("the release %s is not signed by the release key (%v): it was not installed", rel.Tag, err)
		}
	}
	want := checksum(string(sums), name)
	if want == "" {
		return "", fmt.Errorf("SHA256SUMS of %s does not list %s", rel.Tag, name)
	}
	// The download is most of the update: from 5% to 85%.
	u.step(PhaseDownload, 5, 0, 0)
	bin, err := u.fetch(ctx, binURL, maxBinary, "application/octet-stream", func(done, total int64) {
		pct := 5
		if total > 0 {
			pct += int(done * 80 / total)
		}
		u.step(PhaseDownload, min(pct, 85), done, total)
	})
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", name, err)
	}
	u.step(PhaseVerify, 88, 0, 0)
	if sum := sha256.Sum256(bin); hex.EncodeToString(sum[:]) != want {
		return "", fmt.Errorf("%s does not match its SHA256SUMS: it was not installed", name)
	}
	u.step(PhaseInstall, 92, 0, 0)
	if err := u.swap(ctx, bin, rel.Tag); err != nil {
		return "", err
	}
	// Until the new version has held for a while, it can be gone back from.
	_ = writeJSON(u.markerPath(), pending{From: u.Current, To: rel.Tag})
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

// swap checks the new binary runs and says the version it should, then swaps it in: the
// running one is kept as <name>.prev, so a bad update can be put back by hand.
func (u *Updater) swap(ctx context.Context, bin []byte, tag string) error {
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
		if u.progress != nil {
			u.progress.Phase, u.progress.Error = PhaseFailed, u.status.Error
		}
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
