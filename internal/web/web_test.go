package web

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestSub(t *testing.T) {
	built := fstest.MapFS{"dist/index.html": {Data: []byte("<html>")}, "dist/assets/app.js": {Data: []byte("1")}}
	dist, ok := sub(built)
	if !ok {
		t.Fatal("a build with an index.html is the interface")
	}
	if data, err := fs.ReadFile(dist, "assets/app.js"); err != nil || string(data) != "1" {
		t.Errorf("asset: %q %v", data, err)
	}
	// The placeholder alone, or nothing at all, is no interface.
	if _, ok := sub(fstest.MapFS{"dist/placeholder.txt": {Data: []byte("x")}}); ok {
		t.Error("a placeholder is not the interface")
	}
	if _, ok := sub(fstest.MapFS{}); ok {
		t.Error("an empty tree is not the interface")
	}
}

func TestFS(t *testing.T) {
	// Whatever this build carries, FS agrees with it: the placeholder is always there.
	if _, err := fs.Stat(embedded, "dist/placeholder.txt"); err != nil {
		t.Fatalf("the placeholder keeps dist/ embeddable: %v", err)
	}
	dist, ok := FS()
	if _, err := fs.Stat(embedded, "dist/index.html"); (err == nil) != ok || (ok && dist == nil) {
		t.Errorf("FS ok=%v but index.html err=%v", ok, err)
	}
}
