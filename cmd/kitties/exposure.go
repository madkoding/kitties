package main

import (
	"errors"
	"net"
	"os"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/gateway"
)

// exposure is the gateway's reachability (local, lan, wan) as Settings changes it: the choice is
// kept in the data directory and the gateway restarts to listen on the new address.
type exposure struct {
	dataDir string
	listen  string
}

func exposureOf(cfg config.Config, listen string) *exposure {
	return &exposure{dataDir: cfg.DataDirOr(), listen: listen}
}

func (e *exposure) Exposure() gateway.ExposureInfo {
	info := gateway.ExposureInfo{Mode: config.ExposureOf(e.listen), Listen: e.listen, LANIP: config.LANIP(), CanChange: true}
	if _, err := os.Stat("/.dockerenv"); err == nil || os.Getenv("KITTIES_IN_DOCKER") != "" {
		info.CanChange = false
		info.Why = "this gateway runs in a container: set KITTIES_LISTEN and the published port in the compose file"
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
	if _, err := config.ListenFor(mode, port, config.LANIP()); err != nil {
		return err
	}
	return config.SaveExposure(e.dataDir, mode)
}

func (e *exposure) Relaunch() error { return relaunch() }
