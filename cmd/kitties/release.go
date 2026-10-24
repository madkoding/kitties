package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/madkoding/kitties/internal/selfupdate"
)

// The commands of whoever makes the releases: the key the releases are signed with, signing the
// checksums of one, and checking a signature. The pipeline uses them; a gateway only ever checks.

func cmdRelease(cmd string, args []string, out, errOut io.Writer) int {
	switch cmd {
	case "release-key":
		pub, priv, err := selfupdate.GenerateKey()
		if err != nil {
			fmt.Fprintf(errOut, "kitties: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "public key (not secret; build it into the binaries as UPDATE_PUBKEY, or set updates.public_key):\n%s\n\n", pub)
		fmt.Fprintf(out, "private key (a secret: keep it as KITTIES_SIGNING_KEY of the release pipeline and nowhere else):\n%s\n", priv)
		return 0
	case "release-sign":
		if len(args) != 1 {
			fmt.Fprintln(errOut, "usage: kitties release-sign <file>")
			return 2
		}
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Fprintf(errOut, "kitties: %v\n", err)
			return 1
		}
		sig, err := selfupdate.Sign(os.Getenv("KITTIES_SIGNING_KEY"), data)
		if err != nil {
			fmt.Fprintf(errOut, "kitties: KITTIES_SIGNING_KEY: %v\n", err)
			return 1
		}
		if err := os.WriteFile(args[0]+".sig", []byte(sig+"\n"), 0o644); err != nil {
			fmt.Fprintf(errOut, "kitties: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "signed %s: %s.sig\n", args[0], args[0])
		return 0
	case "release-verify":
		if len(args) != 3 {
			fmt.Fprintln(errOut, "usage: kitties release-verify <file> <sig file> <public key>")
			return 2
		}
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Fprintf(errOut, "kitties: %v\n", err)
			return 1
		}
		sig, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintf(errOut, "kitties: %v\n", err)
			return 1
		}
		if err := selfupdate.Verify(strings.TrimSpace(args[2]), data, string(sig)); err != nil {
			fmt.Fprintf(errOut, "kitties: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "%s is signed by that key\n", args[0])
		return 0
	}
	return 2
}
