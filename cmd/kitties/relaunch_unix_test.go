//go:build !windows

package main

import "testing"

func TestRelaunchExecsThisProgram(t *testing.T) {
	var got string
	old := execSelf
	execSelf = func(path string, _ []string, _ []string) error { got = path; return nil }
	t.Cleanup(func() { execSelf = old })
	if err := (&exposure{}).Relaunch(); err != nil || got == "" {
		t.Errorf("Relaunch = %v, exec of %q", err, got)
	}
}
