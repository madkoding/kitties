package main

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"

	"rsc.io/qr"
)

// SIGN IN FROM THE PHONE.
//
// The app (on the machine the gateway runs on) draws a QR code with the page's address and its
// token after a #: a phone's camera opens it and the page signs in by itself. The part after the #
// never reaches a server, and the page takes it off the address at once.

// signInLink is the page at base with the token to sign in with.
func signInLink(base, token string) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("the address has to be like http://192.168.1.20:7488")
	}
	if token == "" {
		return "", errors.New("this app has no token for the gateway")
	}
	return u.String() + "/#signin=" + url.PathEscape(token), nil
}

// SignInQR is a PNG (as a data: URL) of the link that opens the page at base and signs in.
func (a *App) SignInQR(base string) (string, error) {
	link, err := signInLink(base, a.conn.Token)
	if err != nil {
		return "", err
	}
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		return "", err
	}
	code.Scale = 6
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}
