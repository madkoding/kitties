package container

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestStream(t *testing.T) {
	var buf bytes.Buffer
	_ = WriteFrame(&buf, []byte("one"))
	buf.WriteString("PING\n\n")
	_ = WriteFrame(&buf, []byte("two\nlines"))
	r := bufio.NewReader(&buf)
	for _, want := range []string{"one", "two\nlines"} {
		got, err := ReadFrame(r)
		if err != nil || string(got) != want {
			t.Fatalf("frame = %q %v, want %q", got, err, want)
		}
	}
	if _, err := ReadFrame(r); err != io.EOF {
		t.Errorf("the end = %v", err)
	}
	for _, bad := range []string{"HELLO\n", "FRAME x\n", "FRAME 0\n", "FRAME 999999999\n", "FRAME 10\nshort"} {
		if _, err := ReadFrame(bufio.NewReader(strings.NewReader(bad))); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}

	// The command: docker exec -i with --stdin, so closing ends it in the container.
	var args []string
	old := startStream
	startStream = func(_ context.Context, bin string, a ...string) (io.ReadCloser, error) {
		args = append([]string{bin}, a...)
		return io.NopCloser(strings.NewReader("")), nil
	}
	t.Cleanup(func() { startStream = old })
	m := New(Runtime{Bin: "docker"})
	if _, err := m.Stream(context.Background(), "kitty-mimi", 8, 70); err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "docker exec -i kitty-mimi kitty-stream --stdin 8 70" {
		t.Errorf("args = %v", args)
	}
	if _, err := m.Stream(context.Background(), "Bad Name", 8, 70); err == nil {
		t.Error("a bad name")
	}
}

// The real command starts, reads and closes (with a program every system has).
func TestStartStream(t *testing.T) {
	s, err := startStream(context.Background(), "cat")
	if err != nil {
		t.Skip("no cat:", err)
	}
	if err := s.Close(); err != nil {
		t.Error(err)
	}
}
