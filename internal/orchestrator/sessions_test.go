package orchestrator

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/keep"
	"github.com/madkoding/kitties/internal/kitty"
)

// memKeeper keeps backups in memory.
type memKeeper struct {
	data    map[string][]byte
	saveErr error
}

func (k *memKeeper) Save(id string, d []byte) error {
	if k.saveErr != nil {
		return k.saveErr
	}
	k.data[id] = d
	return nil
}
func (k *memKeeper) Load(id string) ([]byte, keep.Info, error) {
	d, ok := k.data[id]
	if !ok {
		return nil, keep.Info{}, keep.ErrNone
	}
	return d, keep.Info{Saved: time.Now(), Bytes: len(d)}, nil
}
func (k *memKeeper) Info(id string) (keep.Info, bool) {
	d, ok := k.data[id]
	return keep.Info{Saved: time.Now(), Bytes: len(d)}, ok
}
func (k *memKeeper) Forget(id string) error { delete(k.data, id); return nil }

func sessionsManager(t *testing.T) (*Manager, *fakeContainers, *memKeeper) {
	t.Helper()
	f := newFake()
	m, err := New(testConfig(t), WithContainers(f, nil))
	if err != nil {
		t.Fatal(err)
	}
	keeper := &memKeeper{data: map[string][]byte{}}
	m.SetKeeper(keeper)
	return m, f, keeper
}

func waitStatus(t *testing.T, m *Manager, id, want string) *kitty.Kitty {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		k, _ := m.Get(id)
		if k.Status == want {
			return k
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %q, want %q (%s)", k.Status, want, k.Error)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The sessions are backed up (the browser closed first), kept away, and given to a container made anew.
func TestSessionsSurviveTheDesktopBeingMadeAnew(t *testing.T) {
	ctx := context.Background()
	m, f, keeper := sessionsManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	tgz := []byte("\x1f\x8bcookies")
	f.outputs = map[string]string{"kitty-backup": base64.StdEncoding.EncodeToString(tgz) + "\n"}
	info, err := m.Backup(ctx, "mimi")
	if err != nil || info.Bytes != len(tgz) || !bytes.Equal(keeper.data[k.ID], tgz) {
		t.Fatalf("Backup = %+v, %v (kept %q)", info, err, keeper.data[k.ID])
	}
	// Stopping closes the browser in an orderly way first, then backs up.
	f.execs = nil
	if err := m.Stop(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.execs, "|"); !strings.Contains(got, "kitty-page quit") || strings.Index(got, "kitty-page quit") > strings.Index(got, "kitty-backup") {
		t.Errorf("stop = %q", got)
	}
	// A container that is made anew gets the backup on its input.
	if err := m.ctr().Remove(ctx, k.Container); err != nil {
		t.Fatal(err)
	}
	f.inputs, f.execs = nil, nil
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	if len(f.inputs) != 1 || f.inputs[0] != k.Container+":"+string(tgz) || !strings.Contains(strings.Join(f.execs, "|"), "kitty-restore") {
		t.Errorf("restore: inputs %q execs %v", f.inputs, f.execs)
	}
	// Starting a container that already exists restores nothing.
	f.inputs = nil
	_ = m.Stop(ctx, "mimi")
	_ = m.Start(ctx, "mimi")
	if len(f.inputs) != 0 {
		t.Errorf("a container that was only stopped was restored: %v", f.inputs)
	}
}

func TestBackupChecks(t *testing.T) {
	ctx := context.Background()
	m, f, keeper := sessionsManager(t)
	no := false
	_, _ = m.Create(ctx, Spec{Name: "mimi"})
	_, _ = m.Create(ctx, Spec{Name: "brain", Desktop: &no})
	if _, err := m.Backup(ctx, "brain"); err == nil || !strings.Contains(err.Error(), "has no desktop") {
		t.Errorf("no desktop: %v", err)
	}
	if _, err := m.Backup(ctx, "nobody"); err == nil {
		t.Error("unknown kitty")
	}
	if _, err := m.Backup(ctx, "mimi"); err == nil || !strings.Contains(err.Error(), "is not running") {
		t.Errorf("not running: %v", err)
	}
	_ = m.Start(ctx, "mimi")
	f.outputs = map[string]string{"kitty-backup": "\n"}
	if _, err := m.Backup(ctx, "mimi"); err == nil || !strings.Contains(err.Error(), "nothing to back up") {
		t.Errorf("empty: %v", err)
	}
	f.outputs = map[string]string{"kitty-backup": "***"}
	if _, err := m.Backup(ctx, "mimi"); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Errorf("garbage: %v", err)
	}
	f.outputs = map[string]string{"kitty-backup": base64.StdEncoding.EncodeToString([]byte("x"))}
	keeper.saveErr = errors.New("disk full")
	if _, err := m.Backup(ctx, "mimi"); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("save failed: %v", err)
	}
	keeper.saveErr = nil
	// An image without kitty-backup says it needs the update.
	f.fail = "exec"
	if _, err := m.Backup(ctx, "mimi"); err == nil || !strings.Contains(err.Error(), "update the desktop") {
		t.Errorf("old image: %v", err)
	}
	f.fail = ""
	m.SetKeeper(nil)
	if _, err := m.Backup(ctx, "mimi"); err == nil || !strings.Contains(err.Error(), "keeps no backups") {
		t.Errorf("no keeper: %v", err)
	}
	// Without a keeper nothing is backed up or restored, and stopping still works.
	if err := m.Stop(ctx, "mimi"); err != nil {
		t.Error(err)
	}
}

func TestDesktopInfo(t *testing.T) {
	ctx := context.Background()
	m, f, keeper := sessionsManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	no := false
	_, _ = m.Create(ctx, Spec{Name: "brain", Desktop: &no})
	if _, err := m.DesktopInfo(ctx, "brain"); err == nil {
		t.Error("a brain has no desktop")
	}
	info, err := m.DesktopInfo(ctx, "mimi")
	if err != nil || info.Image == "" || info.UpdateAvailable || info.Backup != nil {
		t.Errorf("before: %+v %v", info, err)
	}
	_ = m.Start(ctx, "mimi")
	f.stale = true
	keeper.data[k.ID] = []byte("x")
	info, _ = m.DesktopInfo(ctx, "mimi")
	if !info.UpdateAvailable || info.Backup == nil || info.Backup.Bytes != 1 {
		t.Errorf("after: %+v", info)
	}
}

// An update pulls the image, backs up, makes the container anew and gives the sessions back.
func TestUpdateMovesTheDesktopToTheNewestImage(t *testing.T) {
	ctx := context.Background()
	m, f, keeper := sessionsManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	_ = m.Start(ctx, "mimi")
	f.outputs = map[string]string{"kitty-backup": base64.StdEncoding.EncodeToString([]byte("sessions"))}
	f.stale = true
	f.calls, f.execs = nil, nil
	started, err := m.UpdateInBackground("mimi")
	if err != nil || started.Status != kitty.StatusStarting {
		t.Fatalf("UpdateInBackground = %+v, %v", started, err)
	}
	waitStatus(t, m, k.ID, kitty.StatusRunning)
	calls := strings.Join(f.calls, "|")
	if !strings.Contains(calls, "pull ") || strings.Index(calls, "pull") > strings.Index(calls, "rm") || !strings.Contains(calls, "up") {
		t.Errorf("calls = %q", calls)
	}
	if string(keeper.data[k.ID]) != "sessions" || len(f.inputs) != 1 || !strings.HasSuffix(f.inputs[0], ":sessions") {
		t.Errorf("sessions: kept %q inputs %v", keeper.data[k.ID], f.inputs)
	}
	// Already the newest: nothing is made anew, and a failed pull (a local image) does not matter.
	f.stale, f.pullErr = false, errors.New("offline")
	f.calls, f.inputs = nil, nil
	if _, err := m.UpdateInBackground("mimi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, k.ID, kitty.StatusRunning)
	time.Sleep(20 * time.Millisecond)
	if c := strings.Join(f.calls, "|"); strings.Contains(c, "rm") || strings.Contains(c, "up") || len(f.inputs) != 0 {
		t.Errorf("an up-to-date desktop was made anew: %q %v", c, f.inputs)
	}
}

func TestUpdateRefusesAndFails(t *testing.T) {
	ctx := context.Background()
	m, f, _ := sessionsManager(t)
	no := false
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	_, _ = m.Create(ctx, Spec{Name: "brain", Desktop: &no})
	if _, err := m.UpdateInBackground("brain"); err == nil {
		t.Error("a brain has no desktop to update")
	}
	if _, err := m.UpdateInBackground("nobody"); err == nil {
		t.Error("unknown kitty")
	}
	_ = m.Start(ctx, "mimi")
	_, _ = m.Hold("mimi", true)
	if _, err := m.UpdateInBackground("mimi"); err == nil || !strings.Contains(err.Error(), "has the desktop") {
		t.Errorf("held: %v", err)
	}
	_, _ = m.Hold("mimi", false)
	// A container that cannot be removed is a failed kitty with the reason on it.
	f.stale, f.fail = true, "rm"
	if _, err := m.UpdateInBackground("mimi"); err != nil {
		t.Fatal(err)
	}
	got := waitStatus(t, m, k.ID, kitty.StatusError)
	if got.Error == "" {
		t.Error("no reason on the kitty")
	}
	// A runtime that cannot say what exists is a failure too.
	f.fail = "list"
	if err := m.update(ctx, k.ID); err == nil {
		t.Error("update without a list of containers")
	}
	if err := m.update(ctx, "zz"); err == nil {
		t.Error("update of a kitty that is not there")
	}
}
