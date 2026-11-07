package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestPageAddresses(t *testing.T) {
	cases := []struct {
		listen, public string
		https          bool
		lan            string
		want           []string
	}{
		{"127.0.0.1:7488", "", false, "", []string{"http://127.0.0.1:7488"}},
		{"192.168.1.5:7488", "", false, "", []string{"http://127.0.0.1:7488", "http://192.168.1.5:7488"}},
		{"0.0.0.0:7488", "", true, "10.0.0.2", []string{"https://127.0.0.1:7488", "https://10.0.0.2:7488"}},
		{"0.0.0.0:7488", "", false, "", []string{"http://127.0.0.1:7488"}},
		{"127.0.0.1:7488", "https://k.example.com", false, "", []string{"https://k.example.com", "http://127.0.0.1:7488"}},
		{"nonsense", "", false, "", []string{"http://nonsense"}},
	}
	for _, c := range cases {
		if got := pageAddresses(c.listen, c.public, c.https, c.lan); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %s: %v, want %v", c.listen, c.public, got, c.want)
		}
	}
}

func TestPrintWelcome(t *testing.T) {
	var b strings.Builder
	printWelcome(&b, "v1.2.3", "0.0.0.0:7488", "", false, "tok-123", "/data/gateway.token")
	out := b.String()
	for _, want := range []string{"Kitties v1.2.3", "Open:    http://127.0.0.1:7488", "gateway token: tok-123\n", "/data/gateway.token", "kitties token"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
