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

func TestLimits(t *testing.T) {
	if err := (Limits{MaxTokens: 1, MaxSeconds: 2, MaxRounds: 3, Retries: 4}).Validate(); err != nil {
		t.Error(err)
	}
	for _, bad := range []Limits{{MaxTokens: -1}, {MaxSeconds: -1}, {MaxRounds: -1}, {Retries: -1}, {Retries: 11}} {
		if bad.Validate() == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
	d := Limits{MaxTokens: 100, MaxSeconds: 200, MaxRounds: 30, Retries: 1}
	if got := (Limits{MaxSeconds: 5}).Or(d); got != (Limits{MaxTokens: 100, MaxSeconds: 5, MaxRounds: 30, Retries: 1}) {
		t.Errorf("Or = %+v", got)
	}
	if got := (Limits{}).Or(d); got != d {
		t.Errorf("Or empty = %+v", got)
	}
}

func TestValidTheme(t *testing.T) {
	for _, ok := range []string{"", "Numix", "Greybird-dark"} {
		if !ValidTheme(ok) {
			t.Errorf("%q is a theme", ok)
		}
	}
	for _, bad := range []string{"neon", "../Numix", "numix", "win95"} {
		if ValidTheme(bad) {
			t.Errorf("%q is not a theme", bad)
		}
	}
}
