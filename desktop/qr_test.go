package main

import (
	"strings"
	"testing"
)

func TestSignInLink(t *testing.T) {
	got, err := signInLink("http://192.168.1.20:7488/", "abc def")
	if err != nil || got != "http://192.168.1.20:7488/#signin=abc%20def" {
		t.Errorf("link = %q %v", got, err)
	}
	for _, bad := range []string{"", "ftp://x", "192.168.1.20:7488", "http://"} {
		if _, err := signInLink(bad, "tok"); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := signInLink("http://127.0.0.1:7488", ""); err == nil {
		t.Error("a link without a token")
	}
}

func TestSignInQR(t *testing.T) {
	a := &App{conn: Connection{URL: "http://127.0.0.1:7488", Token: "0123456789abcdef0123456789abcdef"}}
	png, err := a.SignInQR("http://192.168.1.20:7488")
	if err != nil || !strings.HasPrefix(png, "data:image/png;base64,") || len(png) < 200 {
		t.Errorf("qr = %.40q %v", png, err)
	}
	if _, err := (&App{}).SignInQR("http://192.168.1.20:7488"); err == nil {
		t.Error("a QR without a token")
	}
}
