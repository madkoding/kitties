package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
)

func TestTransient(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("bad model"), false},
		{context.Canceled, false},
		{fmt.Errorf("x: %w", ErrNoKey), false},
		{&HTTPError{Code: 400}, false},
		{&HTTPError{Code: 401}, false},
		{fmt.Errorf("anthropic/x: %w", &HTTPError{Code: 429}), true},
		{&HTTPError{Code: 529}, true},
		{&net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{fmt.Errorf("read: %w", io.ErrUnexpectedEOF), true},
		{context.DeadlineExceeded, true},
	} {
		if got := Transient(c.err); got != c.want {
			t.Errorf("Transient(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
