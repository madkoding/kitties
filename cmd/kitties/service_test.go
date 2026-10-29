package main

import (
	"errors"
	"io"
	osexec "os/exec"
	"strings"
	"testing"
)

func TestGatewayServiceCommands(t *testing.T) {
	var calls []string
	root, sudo, fail := true, true, false
	oldLook, oldRoot, oldRun := lookPath, isRoot, serviceRunner
	t.Cleanup(func() { lookPath, isRoot, serviceRunner = oldLook, oldRoot, oldRun })
	lookPath = func(name string) (string, error) {
		if name == "sudo" && !sudo {
			return "", osexec.ErrNotFound
		}
		return "/usr/bin/" + name, nil
	}
	isRoot = func() bool { return root }
	serviceRunner = func(name string, args []string, _ io.Reader, _, _ io.Writer) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if fail {
			return errors.New("exit status 3")
		}
		return nil
	}
	last := func() string { return calls[len(calls)-1] }

	for verb, want := range map[string]string{
		"start":   "systemctl start kitties",
		"stop":    "systemctl stop kitties",
		"restart": "systemctl restart kitties",
		"status":  "systemctl status --no-pager kitties",
		"logs":    "journalctl -u kitties -n 100 -f",
	} {
		if code, out, errOut := exec_(t, "gateway", verb); code != 0 {
			t.Errorf("%s: %d %s %s", verb, code, out, errOut)
		}
		if last() != want {
			t.Errorf("%s ran %q, want %q", verb, last(), want)
		}
	}
	exec_(t, "gateway", "logs", "-n", "5")
	if last() != "journalctl -u kitties -n 5" {
		t.Errorf("logs with arguments: %q", last())
	}

	root = false
	exec_(t, "gateway", "restart")
	if last() != "sudo systemctl restart kitties" {
		t.Errorf("not root: %q", last())
	}
	exec_(t, "gateway", "status")
	if last() != "systemctl status --no-pager kitties" {
		t.Errorf("status never needs sudo: %q", last())
	}
	sudo = false
	exec_(t, "gateway", "stop")
	if last() != "systemctl stop kitties" {
		t.Errorf("no sudo: %q", last())
	}

	fail = true
	if code, _, errOut := exec_(t, "gateway", "start"); code != 1 || !strings.Contains(errOut, "failed") {
		t.Errorf("failing start: %d %s", code, errOut)
	}
	if code, _, _ := exec_(t, "gateway", "status"); code != 3 {
		t.Errorf("status of a stopped service must exit 3, got %d", code)
	}
	lookPath = func(string) (string, error) { return "", osexec.ErrNotFound }
	if code, _, errOut := exec_(t, "gateway", "start"); code != 1 || !strings.Contains(errOut, "foreground") {
		t.Errorf("no systemd: %d %s", code, errOut)
	}
}

// exec_ is the exec helper of main_test.go under a name that does not clash with os/exec here.
func exec_(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return exec(t, "", args...)
}
