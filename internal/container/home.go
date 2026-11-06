package container

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// WHAT OUTLIVES A CONTAINER.
//
// A kitty's home (/home/kitty) is a folder of the host when the image knows how to seed one
// (HomeLabel): its settings, the programs it put in ~/.local or /opt/kitty-apps and its Shared
// folder survive an update of the desktop. These are the runtime's part of it: what an image
// supports, what a container has mounted, and copying a home in and out of a container as a tar
// stream (never whole in memory: a home can be big).

// runStream runs a command with stdin and stdout connected to r and w; it answers what the
// command wrote on its standard error. A variable, so a test says what the machine does.
var runStream = func(ctx context.Context, r io.Reader, w io.Writer, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r, w, &stderr
	err := cmd.Run()
	return stderr.Bytes(), err
}

// ImageHasHome tells whether image seeds a home mounted from the host. An image that is not on
// this machine (not pulled yet) answers false: its container is made with the image's own home,
// and gets a host home when it is made anew.
func (m *Manager) ImageHasHome(ctx context.Context, image string) (bool, error) {
	if !imageRef.MatchString(image) {
		return false, fmt.Errorf("image %q is not a valid reference", image)
	}
	out, err := runCmd(ctx, m.rt.Bin, "image", "inspect", "-f", `{{index .Config.Labels "`+HomeLabel+`"}}`, image)
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// Mounts lists where folders are mounted inside a container.
func (m *Manager) Mounts(ctx context.Context, name string) ([]string, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("container name %q is not valid", name)
	}
	out, err := runCmd(ctx, m.rt.Bin, "inspect", "-f", "{{range .Mounts}}{{println .Destination}}{{end}}", name)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %s", name, tail(string(out), 4))
	}
	var dests []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			dests = append(dests, line)
		}
	}
	return dests, nil
}

// HasHome tells whether a container has its home mounted from the host.
func (m *Manager) HasHome(ctx context.Context, name string) (bool, error) {
	dests, err := m.Mounts(ctx, name)
	if err != nil {
		return false, err
	}
	for _, d := range dests {
		if d == HomeDir {
			return true, nil
		}
	}
	return false, nil
}

// ExecOut runs a command inside the container and copies what it prints to w.
func (m *Manager) ExecOut(ctx context.Context, name string, w io.Writer, argv ...string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("container name %q is not valid", name)
	}
	if len(argv) == 0 {
		return errors.New("nothing to run")
	}
	args := append([]string{"exec", "-w", HomeDir, name}, argv...)
	if stderr, err := runStream(ctx, nil, w, m.rt.Bin, args...); err != nil {
		return fmt.Errorf("exec in %s: %s", name, firstLine(stderr, err))
	}
	return nil
}

// ExecIn runs a command inside the container with r on its standard input.
func (m *Manager) ExecIn(ctx context.Context, name string, r io.Reader, argv ...string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("container name %q is not valid", name)
	}
	if len(argv) == 0 {
		return "", errors.New("nothing to run")
	}
	var out bytes.Buffer
	args := append([]string{"exec", "-i", "-w", HomeDir, name}, argv...)
	if stderr, err := runStream(ctx, r, &out, m.rt.Bin, args...); err != nil {
		return out.String(), fmt.Errorf("exec in %s: %s", name, firstLine(stderr, err))
	}
	return out.String(), nil
}

// Restart stops and starts a container again: its desktop reads its home anew.
func (m *Manager) Restart(ctx context.Context, name string) error {
	return m.simple(ctx, "restart", "-t", "10", name)
}

// firstLine is the reason a command gave on its standard error, else its error.
func firstLine(stderr []byte, err error) string {
	if s := tail(string(stderr), 3); s != "" {
		return s
	}
	return err.Error()
}
