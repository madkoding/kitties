package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/madkoding/kitties/internal/config"
)

// THE FIRST THING THE CONSOLE SHOWS.
//
// Whoever started the gateway is the one who should get in: the console (or the service's log)
// gets where to open the page and the token to sign in with, set apart from the rest of the
// output so it is found at a glance. The token is the page's only password, so it is said here
// and nowhere a visitor could see it.

// pageAddresses are the addresses a person opens the page at, best first: the public one, then
// the ones this machine answers on (127.0.0.1 always, the network address when it listens there).
func pageAddresses(listen, publicURL string, https bool, lanIP string) []string {
	scheme := "http"
	if https {
		scheme = "https"
	}
	var out []string
	if publicURL != "" {
		out = append(out, publicURL)
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return append(out, scheme+"://"+listen)
	}
	out = append(out, scheme+"://"+net.JoinHostPort("127.0.0.1", port))
	switch config.ExposureOf(listen) {
	case config.ExposureLAN:
		host, _, _ := net.SplitHostPort(listen)
		out = append(out, scheme+"://"+net.JoinHostPort(host, port))
	case config.ExposureWAN:
		if lanIP != "" {
			out = append(out, scheme+"://"+net.JoinHostPort(lanIP, port))
		}
	}
	return out
}

// printWelcome writes the block with the addresses and the token.
func printWelcome(w io.Writer, version, listen, publicURL string, https bool, token, tokenPath string) {
	lan := ""
	if config.ExposureOf(listen) == config.ExposureWAN {
		lan = config.LANIP()
	}
	addrs := pageAddresses(listen, publicURL, https, lan)
	line := "  ------------------------------------------------------------------"
	fmt.Fprintln(w)
	fmt.Fprintln(w, line)
	fmt.Fprintf(w, "   Kitties %s is starting\n", version)
	fmt.Fprintln(w)
	for i, a := range addrs {
		label := "            "
		if i == 0 {
			label = "   Open:    "
		}
		fmt.Fprintf(w, "%s%s\n", label, a)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "   gateway token: %s\n", token)
	fmt.Fprintln(w, "   It is the page's password: paste it in the browser to sign in.")
	fmt.Fprintf(w, "   Only this console and %s have it.\n", tokenPath)
	fmt.Fprintf(w, "   See it again any time with: %s token\n", progName())
	fmt.Fprintln(w, line)
	fmt.Fprintln(w)
}

// progName is how this program was started (kitties, kitties-gateway), for the hints it prints.
func progName() string {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if name == "" || name == "." || strings.HasSuffix(name, ".test") {
		return "kitties"
	}
	return name
}
