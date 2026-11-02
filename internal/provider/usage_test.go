package provider

import "testing"

func TestCountedWeighsCachedTokensAtATenth(t *testing.T) {
	for _, c := range []struct {
		u    Usage
		want int
	}{
		{Usage{Input: 100, Output: 20}, 120},
		{Usage{Input: 1000, Output: 50, CacheRead: 900}, 100 + 90 + 50},
		{Usage{Input: 1000, Output: 50, CacheRead: 400, CacheWrite: 500}, 600 + 40 + 50},
		{Usage{Input: 10, CacheRead: 500}, 1}, // a cache read can never exceed the prompt
	} {
		if got := c.u.Counted(); got != c.want {
			t.Errorf("%+v counts %d, want %d", c.u, got, c.want)
		}
	}
}
