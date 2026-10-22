package kitty

import "testing"

func TestContainerName(t *testing.T) {
	if ContainerName("mimi") != "kitty-mimi" {
		t.Error(ContainerName("mimi"))
	}
	k := Kitty{Name: "x", Status: StatusCreated}
	if k.Status != "created" || StatusRunning != "running" || StatusStopped != "stopped" || StatusError != "error" {
		t.Error("statuses")
	}
}

func TestAvatar(t *testing.T) {
	a := DefaultAvatar("mimi")
	if a.Breed == "" || a.Color == "" || a != DefaultAvatar("mimi") {
		t.Error("the default is stable")
	}
	seen := map[Avatar]bool{}
	for _, n := range []string{"mimi", "tom", "luna", "zoe", "ana", "bo", "cy"} {
		seen[DefaultAvatar(n)] = true
	}
	if len(seen) < 4 {
		t.Errorf("names spread over the avatars: %v", seen)
	}
	got, err := (Avatar{Breed: " Tuxedo ", Color: "PINK"}).Normalize("x")
	if err != nil || got != (Avatar{Breed: "tuxedo", Color: "pink"}) {
		t.Errorf("Normalize = %+v %v", got, err)
	}
	if got, err := (Avatar{}).Normalize("mimi"); err != nil || got != DefaultAvatar("mimi") {
		t.Error("empty takes the default")
	}
	if _, err := (Avatar{Breed: "lion"}).Normalize("x"); err == nil {
		t.Error("unknown breed")
	}
	if _, err := (Avatar{Color: "plaid"}).Normalize("x"); err == nil {
		t.Error("unknown color")
	}
}
