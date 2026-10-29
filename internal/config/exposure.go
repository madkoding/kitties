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
	return os.WriteFile(filepath.Join(dataDir, ExposureFile), []byte(mode+"\n"), 0o600)
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
