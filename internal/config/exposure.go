package config

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// Exposure is where the gateway can be reached from, in three plain choices: this machine alone,
// the local network, or every interface (the internet too when the machine is public).
const (
	ExposureLocal = "local" // 127.0.0.1
	ExposureLAN   = "lan"   // the address of this machine in the local network
	ExposureWAN   = "wan"   // 0.0.0.0
)

// ExposureFile is the file in the data directory that keeps the choice made from the page or an
// installer. It wins over gateway.listen and KITTIES_LISTEN, so a person's choice from Settings
// holds even where the service's environment file says another address.
const ExposureFile = "exposure"

// ExposureAutoFile marks an exposure the gateway chose by itself on its first start (the local
// network, so it can be set up from another computer), not the person. Settings says so and
// offers to go back to this machine alone; any choice of the person removes it.
const ExposureAutoFile = "exposure.auto"

// DefaultListen is the gateway's address when nobody set one.
const DefaultListen = "127.0.0.1:7488"

// ValidExposure reports whether mode is one of the three.
func ValidExposure(mode string) bool {
	return mode == ExposureLocal || mode == ExposureLAN || mode == ExposureWAN
}

// ExposureOf says which of the three a listen address (host:port) is. Any address that is not
// loopback or the wildcard counts as the local network.
func ExposureOf(listen string) string {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return ExposureLocal
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return ExposureWAN
	}
	if IsLoopbackAddr(listen) {
		return ExposureLocal
	}
	return ExposureLAN
}

// ListenFor is the listen address of a mode on a port; lanIP is the machine's address in the
// local network (needed by ExposureLAN).
func ListenFor(mode, port, lanIP string) (string, error) {
	switch mode {
	case ExposureLocal:
		return net.JoinHostPort("127.0.0.1", port), nil
	case ExposureLAN:
		if lanIP == "" {
			return "", errors.New("could not find this machine's address in the local network")
		}
		return net.JoinHostPort(lanIP, port), nil
	case ExposureWAN:
		return net.JoinHostPort("0.0.0.0", port), nil
	}
	return "", errors.New("exposure must be local, lan or wan")
}

// LANIP is the address this machine has in the local network, or "" when it has none. Nothing is
// sent: a UDP "connection" only asks the system which address it would use.
func LANIP() string {
	c, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return ""
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok && !a.IP.IsLoopback() {
		return a.IP.String()
	}
	return ""
}

// SavedExposure reads the choice kept in dataDir ("" when there is none or it is not valid).
func SavedExposure(dataDir string) string {
	data, err := os.ReadFile(filepath.Join(dataDir, ExposureFile))
	if err != nil {
		return ""
	}
	if m := strings.TrimSpace(string(data)); ValidExposure(m) {
		return m
	}
	return ""
}

// SaveExposure keeps the choice in dataDir.
func SaveExposure(dataDir, mode string) error {
	if !ValidExposure(mode) {
		return errors.New("exposure must be local, lan or wan")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dataDir, ExposureAutoFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, ExposureFile), []byte(mode+"\n"), 0o600)
}

// ExposureAuto reports whether the saved exposure is the one the first start chose by itself.
func ExposureAuto(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, ExposureAutoFile))
	return err == nil
}

// FirstStartLAN opens a gateway that never ran to the local network, so a server without a
// screen can be set up from another computer: the page asks for the token, which only the
// console (and the token file) has. It does so only where nobody chose: no exposure saved, no
// token kept yet (the gateway never started with this data directory), the default address,
// and no public URL, https or proxy in front (those are set up on purpose). lanIP is this
// machine's address in the local network; without one nothing changes. It keeps the choice,
// marked as the gateway's own, and reports the new listen address ("" when nothing changed).
func (c *Config) FirstStartLAN(lanIP string) (string, error) {
	g := c.Gateway
	dir := c.DataDirOr()
	if lanIP == "" || g.Listen != DefaultListen || g.PublicURL != "" || g.TLSCert != "" || g.TrustProxy || SavedExposure(dir) != "" {
		return "", nil
	}
	if _, err := os.Stat(filepath.Join(dir, "gateway.token")); err == nil {
		return "", nil
	}
	_, port, _ := net.SplitHostPort(g.Listen)
	listen, err := ListenFor(ExposureLAN, port, lanIP)
	if err != nil {
		return "", err
	}
	if err := SaveExposure(dir, ExposureLAN); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, ExposureAutoFile), []byte("first start\n"), 0o600); err != nil {
		return "", err
	}
	c.Gateway.Listen = listen
	return listen, nil
}

// ApplyExposure makes the saved choice, if any, the gateway's listen address, keeping the port.
// A LAN choice on a machine with no network address leaves the address as it was.
func (c *Config) ApplyExposure() {
	mode := SavedExposure(c.DataDirOr())
	if mode == "" {
		return
	}
	_, port, err := net.SplitHostPort(c.Gateway.Listen)
	if err != nil {
		return
	}
	if listen, err := ListenFor(mode, port, LANIP()); err == nil {
		c.Gateway.Listen = listen
	}
}
