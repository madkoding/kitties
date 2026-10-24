package vault

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretsAreKeptSealedAndOnlyForTheirKitties(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("  Shop   password ", "hunter2", []string{"k1"}); err != nil {
		t.Fatal(err)
	}
	// On disk it is sealed, and the files are the owner's alone.
	for _, f := range []string{"vault.json", "vault.key"} {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", f, info, err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "vault.json"))
	if bytes.Contains(raw, []byte("hunter2")) {
		t.Error("the secret is on disk in the clear")
	}
	// The name is found however it is cased and spaced; only a kitty that may use it gets it.
	if v, err := s.Get("shop password", "k1"); err != nil || v != "hunter2" {
		t.Errorf("Get = %q, %v", v, err)
	}
	if _, err := s.Get("shop password", "k2"); err == nil {
		t.Error("another kitty opened it")
	}
	if _, err := s.Get("nothing", "k1"); err == nil {
		t.Error("a secret that is not there was opened")
	}
	if got := s.Names("k1"); len(got) != 1 || got[0] != "Shop password" {
		t.Errorf("Names = %v", got)
	}
	if got := s.Names("k2"); len(got) != 0 {
		t.Errorf("k2 sees %v", got)
	}
	// It survives a reopen, and the list never carries values.
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := again.Get("Shop password", "k1"); v != "hunter2" {
		t.Errorf("after reopening: %q", v)
	}
	if l := again.List(); len(l) != 1 || l[0].Name != "Shop password" || l[0].Kitties[0] != "k1" {
		t.Errorf("List = %+v", l)
	}
}

func TestChangeAndForget(t *testing.T) {
	s, _ := Open(t.TempDir())
	_ = s.Put("a", "1", []string{"k1"})
	_ = s.Put("b", "2", []string{"k1", "k2"})
	if err := s.SetKitties("a", []string{"k2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a", "k1"); err == nil {
		t.Error("k1 still opens it")
	}
	if err := s.SetKitties("zz", nil); err == nil {
		t.Error("an unknown secret changed")
	}
	if err := s.ForgetKitty("k2"); err != nil {
		t.Fatal(err)
	}
	if got := s.Names("k2"); len(got) != 0 {
		t.Errorf("a removed kitty still has %v", got)
	}
	// A secret is replaced by saving it again, and deleted.
	_ = s.Put("B", "3", []string{"k1"})
	if v, _ := s.Get("b", "k1"); v != "3" || len(s.List()) != 2 {
		t.Errorf("replace: %q %d", v, len(s.List()))
	}
	if err := s.Delete("b"); err != nil || s.Delete("b") == nil {
		t.Error("delete")
	}
}

func TestChecks(t *testing.T) {
	s, _ := Open(t.TempDir())
	for _, c := range []struct{ name, value string }{{"", "x"}, {strings.Repeat("n", 61), "x"}, {"a", ""}, {"a", strings.Repeat("v", MaxValue+1)}} {
		if err := s.Put(c.name, c.value, nil); err == nil {
			t.Errorf("Put(%d chars, %d chars) was accepted", len(c.name), len(c.value))
		}
	}
}

func TestKeys(t *testing.T) {
	// The key from the environment is used, and a vault made with another does not open.
	dir := t.TempDir()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	t.Setenv(KeyEnv, key)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Put("a", "1", []string{"k"})
	if _, err := os.Stat(filepath.Join(dir, "vault.key")); err == nil {
		t.Error("a key file was made beside an environment key")
	}
	t.Setenv(KeyEnv, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))
	other, _ := Open(dir)
	if _, err := other.Get("a", "k"); err == nil || !strings.Contains(err.Error(), "cannot be opened") {
		t.Errorf("another key opened it: %v", err)
	}
	t.Setenv(KeyEnv, "nope")
	if _, err := Open(dir); err == nil {
		t.Error("a bad environment key was accepted")
	}
	// A bad key file or vault file is refused, not replaced.
	t.Setenv(KeyEnv, "")
	bad := t.TempDir()
	_ = os.WriteFile(filepath.Join(bad, "vault.key"), []byte("short"), 0o600)
	if _, err := Open(bad); err == nil {
		t.Error("a bad key file was accepted")
	}
	worse := t.TempDir()
	if _, err := Open(worse); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(worse, "vault.json"), []byte("{"), 0o600)
	if _, err := Open(worse); err == nil {
		t.Error("a broken vault was accepted")
	}
}

func TestBoxSealsForOneUseOnly(t *testing.T) {
	b, err := NewBox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b.Seal([]byte("cookies"), []byte("backup:k1"))
	if err != nil || bytes.Contains(sealed, []byte("cookies")) {
		t.Fatalf("Seal = %q, %v", sealed, err)
	}
	if plain, err := b.Open(sealed, []byte("backup:k1")); err != nil || string(plain) != "cookies" {
		t.Errorf("Open = %q, %v", plain, err)
	}
	if _, err := b.Open(sealed, []byte("backup:k2")); err == nil {
		t.Error("opened for another use")
	}
	if _, err := b.Open([]byte("x"), nil); err == nil {
		t.Error("opened a truncated blob")
	}
	t.Setenv(KeyEnv, "nope")
	if _, err := NewBox(t.TempDir()); err == nil {
		t.Error("a bad key made a box")
	}
}
