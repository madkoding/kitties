// Package shared reads what the kitties share with their person: each kitty's ~/Shared is a
// folder of the host (<data>/shared/<name>, mounted into its desktop), and the page lists,
// previews, downloads and deletes the files in it. The kitty writes them (kitty-share, or any
// program); the gateway only reads, and never follows a link out of a kitty's folder.
package shared

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/artifacts"
)

// File is one shared file.
type File struct {
	// Kitty is the kitty whose folder it is in.
	Kitty string `json:"kitty"`
	// Path is where it is in that folder, with / between folders.
	Path      string    `json:"path"`
	Type      string    `json:"type"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store is the root that holds every kitty's folder.
type Store struct {
	root string
}

// New opens the folders under root.
func New(root string) *Store { return &Store{root: root} }

const (
	// maxDepth is how deep the folders inside Shared are looked into.
	maxDepth = 4
	// maxFiles is how many files of one kitty are listed.
	maxFiles = 2000
)

var (
	partRe  = regexp.MustCompile(`^[^/\\\x00-\x1f]{1,200}$`)
	kittyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	// ErrNotFound is a file that is not there (or not a plain file).
	ErrNotFound = errors.New("no such shared file")
)

// ValidPath reports whether p is a path inside a kitty's folder: plain names, no hidden ones,
// nothing that climbs out.
func ValidPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Count(p, "/") >= maxDepth {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if !partRe.MatchString(part) || strings.HasPrefix(part, ".") || part == ".." {
			return false
		}
	}
	return true
}

func (s *Store) dir(kitty string) (string, error) {
	if !kittyRe.MatchString(kitty) {
		return "", fmt.Errorf("%q is not a kitty name", kitty)
	}
	return filepath.Join(s.root, kitty), nil
}

// List returns the files the kitties share, the newest first.
func (s *Store) List(kitties []string) []File {
	out := []File{}
	for _, k := range kitties {
		dir, err := s.dir(k)
		if err != nil {
			continue
		}
		n := 0
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(dir, p)
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || strings.Count(rel, "/") >= maxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || !ValidPath(rel) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if n++; n > maxFiles {
				return filepath.SkipAll
			}
			out = append(out, File{Kitty: k, Path: rel, Type: artifacts.Type(rel), Size: info.Size(), UpdatedAt: info.ModTime().UTC()})
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Kitty+"/"+out[i].Path < out[j].Kitty+"/"+out[j].Path
	})
	return out
}

// open opens the kitty's folder as a root: nothing in it, links included, reaches outside it.
func (s *Store) open(kitty, p string) (*os.Root, error) {
	dir, err := s.dir(kitty)
	if err != nil {
		return nil, err
	}
	if !ValidPath(p) {
		return nil, fmt.Errorf("%q is not a path inside the Shared folder", p)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, ErrNotFound
	}
	return r, nil
}

// Open opens a shared file for reading; the caller closes it.
func (s *Store) Open(kitty, p string) (*os.File, fs.FileInfo, error) {
	r, err := s.open(kitty, p)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	f, err := r.Open(p)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, ErrNotFound
	}
	return f, info, nil
}

// Delete removes a shared file.
func (s *Store) Delete(kitty, p string) error {
	r, err := s.open(kitty, p)
	if err != nil {
		return err
	}
	defer r.Close()
	if info, err := r.Lstat(p); err != nil || !info.Mode().IsRegular() {
		return ErrNotFound
	}
	if err := r.Remove(p); err != nil {
		return fmt.Errorf("delete %s: %w", path.Base(p), err)
	}
	return nil
}
