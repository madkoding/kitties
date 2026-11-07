package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/config"
)

func TestExposureSetsAndReadsTheChoice(t *testing.T) {
	t.Setenv("KITTIES_IN_DOCKER", "")
	if _, err := os.Stat("/.dockerenv"); err == nil {
		t.Skip("running in a container: the exposure is read-only there")
	}
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	e := exposureOf(cfg, "127.0.0.1:9100")
	if info := e.Exposure(); info.Mode != config.ExposureLocal || !info.CanChange || info.Listen != "127.0.0.1:9100" {
		t.Fatalf("exposure = %+v", info)
	}
	if err := e.SetExposure(config.ExposureWAN); err != nil {
		t.Fatal(err)
	}
	if got := config.SavedExposure(dir); got != config.ExposureWAN {
		t.Errorf("saved %q", got)
	}
	if err := e.SetExposure("moon"); err == nil {
		t.Error("an unknown mode was accepted")
	}
	if err := exposureOf(cfg, "nonsense").SetExposure(config.ExposureLocal); err == nil {
		t.Error("an address without a port was accepted")
	}
}

func TestExposureIsReadOnlyInAContainer(t *testing.T) {
	t.Setenv("KITTIES_IN_DOCKER", "1")
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	e := exposureOf(cfg, "0.0.0.0:7488")
	if info := e.Exposure(); info.CanChange || info.Why == "" || info.Mode != config.ExposureWAN {
		t.Fatalf("exposure = %+v", info)
	}
	if err := e.SetExposure(config.ExposureLocal); err == nil {
		t.Error("a container changed its exposure")
	}
}

func TestRotateGatewayToken(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	e := exposureOf(cfg, "127.0.0.1:7488")
	if info := e.Exposure(); !info.TokenRotate || info.TokenWhy != "" {
		t.Fatalf("exposure = %+v", info)
	}
	before, _, _ := gatewayToken(cfg)
	if err := e.RotateGatewayToken(); err != nil {
		t.Fatal(err)
	}
	after, _, fresh := gatewayToken(cfg)
	if fresh || after == before || len(after) < 16 {
		t.Errorf("the new token: %q (fresh %v, before %q)", after, fresh, before)
	}
	// A token from the configuration is not replaced by a file.
	cfg.Gateway.Token = "configured-token-0123456789"
	fixed := exposureOf(cfg, "127.0.0.1:7488")
	if info := fixed.Exposure(); info.TokenRotate || info.TokenWhy == "" {
		t.Fatalf("fixed exposure = %+v", info)
	}
	if err := fixed.RotateGatewayToken(); err == nil {
		t.Error("a configured token was rotated")
	}
}

func TestFirstStartListensOnTheLAN(t *testing.T) {
	withRuntime(t, false)
	t.Setenv("KITTIES_IN_DOCKER", "")
	if inContainer() {
		t.Skip("running in a container: the compose file decides where it listens")
	}
	old := lanIP
	t.Cleanup(func() { lanIP = old })
	// Another loopback address stands in for the network one: the test opens nothing.
	lanIP = func() string { return "127.0.0.2" }
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("KITTIES_CONFIG", "")
	t.Setenv("KITTIES_LISTEN", "")
	t.Setenv("KITTIES_DATA_DIR", filepath.Join(dir, "data"))
	wd, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(wd) })
	addr, out, _, stop := serve(t, "gateway")
	stop()
	if addr != "127.0.0.2:7488" || !strings.Contains(out.String(), "First start: Kitties listens on your local network (http://127.0.0.2:7488)") {
		t.Errorf("first start on %s:\n%s", addr, out.String())
	}
	data := filepath.Join(dir, "data")
	if config.SavedExposure(data) != config.ExposureLAN || !config.ExposureAuto(data) {
		t.Error("the first start's exposure is not kept as automatic")
	}
	e := exposureOf(config.Config{DataDir: data}, "192.168.1.20:7488")
	if info := e.Exposure(); info.Mode != config.ExposureLAN || !info.Auto {
		t.Errorf("Settings sees %+v", info)
	}
	if err := e.SetExposure(config.ExposureLocal); err != nil || config.ExposureAuto(data) {
		t.Errorf("going back to local: %v", err)
	}
}
