package selfupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// WHO MADE A RELEASE.
//
// A checksum proves a file arrived whole; it comes from the same place as the file, so it proves
// nothing about who made it. The releases are therefore signed: SHA256SUMS.sig is an Ed25519
// signature of SHA256SUMS, made in the release pipeline with a private key kept as a secret, and
// checked here with the public key built into the binary (or set as updates.public_key). A binary
// is only installed when the signature on the checksums that list it is good.

// builtinKey is the public key the release was built with (-X ...selfupdate.builtinKey=<base64>).
var builtinKey = ""

// GenerateKey makes a signing key pair: the public key to build into the binary, and the private
// key to keep as a secret of the release pipeline. Both are base64.
func GenerateKey() (public, private string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv), nil
}

func decodeKey(s string, size int, what string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != size {
		return nil, fmt.Errorf("the %s is not %d bytes in base64", what, size)
	}
	return raw, nil
}

// Sign signs data with a private key; the signature is base64.
func Sign(private string, data []byte) (string, error) {
	raw, err := decodeKey(private, ed25519.PrivateKeySize, "private key")
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(raw), data)), nil
}

// Verify checks a base64 signature of data against a base64 public key.
func Verify(public string, data []byte, signature string) error {
	raw, err := decodeKey(public, ed25519.PublicKeySize, "public key")
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("the signature is not 64 bytes in base64")
	}
	if !ed25519.Verify(ed25519.PublicKey(raw), data, sig) {
		return errors.New("the signature is not the release key's")
	}
	return nil
}

// publicKey is the key releases are checked with: the configuration's, else the one built in.
func (u *Updater) publicKey() string {
	if k := strings.TrimSpace(u.cfg.PublicKey); k != "" {
		return k
	}
	return strings.TrimSpace(builtinKey)
}
