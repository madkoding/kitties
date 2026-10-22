package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoChatWithoutKitties(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	solo, _ := s.Create("", []string{"ana"})
	pair, _ := s.Create("pair", []string{"ana", "bo"})
	// The last kitty of a chat cannot leave it: the chat is deleted instead.
	if _, err := s.RemoveMember(solo.ID, "ana"); err == nil || !strings.Contains(err.Error(), "at least one kitty") {
		t.Errorf("last member: %v", err)
	}
	// A removed kitty leaves every chat; the ones left with nobody go.
	if err := s.LeaveChats("a1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(solo.ID); err == nil {
		t.Error("a chat with no kitty left must be deleted")
	}
	if _, err := os.Stat(filepath.Join(s.dir, solo.ID+".json")); !os.IsNotExist(err) {
		t.Errorf("its file stays: %v", err)
	}
	got, err := s.Get(pair.ID)
	if err != nil || strings.Join(got.Members, ",") != "b1" {
		t.Errorf("pair = %+v %v", got, err)
	}
	// After a restart the change holds.
	again, err := New(f, s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if list := again.List(); len(list) != 1 || strings.Join(list[0].Members, ",") != "b1" {
		t.Errorf("after restart: %+v", list)
	}
	// Prune with every member known changes nothing.
	if err := again.Prune(func(string) bool { return true }); err != nil || len(again.List()) != 1 {
		t.Error("prune with nothing to prune")
	}
	// Prune drops what no longer exists.
	if err := again.Prune(func(string) bool { return false }); err != nil || len(again.List()) != 0 {
		t.Errorf("prune everything: %v %d", err, len(again.List()))
	}
}
