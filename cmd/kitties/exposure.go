package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/gateway"
)

// exposure is the gateway's reachability (local, lan, wan) as Settings changes it: the choice is
// kept in the data directory and the gateway restarts to listen on the new address.
type exposure struct {
	dataDir string
	listen  string
	// tokenFixed: the token comes from the configuration or KITTIES_TOKEN, which a new file
	// would not replace.
	tokenFixed bool
}

func exposureOf(cfg config.Config, listen string) *exposure {
	return &exposure{dataDir: cfg.DataDirOr(), listen: listen, tokenFixed: cfg.Gateway.Token != ""}
}

// lanIP is this machine's address in the local network; a variable for the tests.
var lanIP = config.LANIP

// inContainer reports whether the gateway runs in a container, where the compose file decides
// where it listens.
func inContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil || os.Getenv("KITTIES_IN_DOCKER") != ""
}

func (e *exposure) Exposure() gateway.ExposureInfo {
	info := gateway.ExposureInfo{Mode: config.ExposureOf(e.listen), Listen: e.listen, LANIP: lanIP(), CanChange: true}
	info.Auto = info.Mode == config.ExposureLAN && config.ExposureAuto(e.dataDir)
	if inContainer() {
		info.CanChange = false
		info.Why = "this gateway runs in a container: set KITTIES_LISTEN and the published port in the compose file"
	}
	info.TokenRotate = !e.tokenFixed
	if e.tokenFixed {
		info.TokenWhy = "the token is set in the configuration (gateway.token or KITTIES_TOKEN): change it there and restart"
	}
	return info
}

func (e *exposure) SetExposure(mode string) error {
	if !e.Exposure().CanChange {
		return errors.New(e.Exposure().Why)
	}
	_, port, err := net.SplitHostPort(e.listen)
	if err != nil {
		return err
	}
	if _, err := config.ListenFor(mode, port, lanIP()); err != nil {
		return err
	}
	return config.SaveExposure(e.dataDir, mode)
}

// RotateGatewayToken writes a new token where the gateway reads it; the restart that follows
// signs every browser and program that used the old one out.
func (e *exposure) RotateGatewayToken() error {
	if e.tokenFixed {
		return errors.New(e.Exposure().TokenWhy)
	}
	if err := os.MkdirAll(e.dataDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(e.dataDir, "gateway.token"), []byte(gateway.NewToken()+"\n"), 0o600)
}

func (e *exposure) Relaunch() error { return relaunch() }
