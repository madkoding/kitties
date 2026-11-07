package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExposure(t *testing.T) {
	for listen, want := range map[string]string{
		"127.0.0.1:7488": ExposureLocal, "localhost:7488": ExposureLocal, "[::1]:7488": ExposureLocal,
		"192.168.1.20:7488": ExposureLAN, "0.0.0.0:7488": ExposureWAN, ":7488": ExposureWAN, "[::]:7488": ExposureWAN,
	} {
		if got := ExposureOf(listen); got != want {
			t.Errorf("ExposureOf(%q) = %q, want %q", listen, got, want)
		}
	}
	for _, c := range []struct{ mode, ip, want string }{
		{ExposureLocal, "", "127.0.0.1:80"}, {ExposureLAN, "10.0.0.5", "10.0.0.5:80"}, {ExposureWAN, "", "0.0.0.0:80"},
	} {
		if got, err := ListenFor(c.mode, "80", c.ip); err != nil || got != c.want {
			t.Errorf("ListenFor(%s) = %q, %v", c.mode, got, err)
		}
	}
	if _, err := ListenFor(ExposureLAN, "80", ""); err == nil {
		t.Error("lan without an address should fail")
	}
	if _, err := ListenFor("moon", "80", ""); err == nil {
		t.Error("an unknown mode should fail")
	}
}

func TestApplyExposure(t *testing.T) {
	dir := t.TempDir()
	c := Defaults()
	c.DataDir = dir
	c.Gateway.Listen = "127.0.0.1:9000"
	c.ApplyExposure()
	if c.Gateway.Listen != "127.0.0.1:9000" {
		t.Errorf("nothing saved changed it to %q", c.Gateway.Listen)
	}
	if err := SaveExposure(dir, ExposureWAN); err != nil {
		t.Fatal(err)
	}
	c.ApplyExposure()
	if c.Gateway.Listen != "0.0.0.0:9000" {
		t.Errorf("saved wan gave %q", c.Gateway.Listen)
	}
	if SaveExposure(dir, "moon") == nil {
		t.Error("an unknown mode was saved")
	}
}

func TestFirstStartLAN(t *testing.T) {
	fresh := func() Config {
		c := Defaults()
		c.DataDir = t.TempDir()
		return c
	}
	c := fresh()
	listen, err := c.FirstStartLAN("192.168.1.20")
	if err != nil || listen != "192.168.1.20:7488" || c.Gateway.Listen != listen {
		t.Fatalf("first start = %q, %v (listen %q)", listen, err, c.Gateway.Listen)
	}
	if SavedExposure(c.DataDir) != ExposureLAN || !ExposureAuto(c.DataDir) {
		t.Error("the first start's choice is kept and marked as the gateway's own")
	}
	// The person's own choice removes the mark.
	if err := SaveExposure(c.DataDir, ExposureLocal); err != nil || ExposureAuto(c.DataDir) {
		t.Errorf("a chosen exposure is still marked automatic: %v", err)
	}
	// Where somebody chose, or it is not the first start, nothing changes.
	for name, set := range map[string]func(*Config){
		"no lan address":  func(c *Config) {},
		"another address": func(c *Config) { c.Gateway.Listen = "127.0.0.1:9000" },
		"public url":      func(c *Config) { c.Gateway.PublicURL = "https://k.example.com" },
		"https":           func(c *Config) { c.Gateway.TLSCert = "cert.pem" },
		"proxy":           func(c *Config) { c.Gateway.TrustProxy = true },
		"saved choice":    func(c *Config) { _ = SaveExposure(c.DataDir, ExposureLocal) },
		"ran before": func(c *Config) {
			_ = os.WriteFile(filepath.Join(c.DataDir, "gateway.token"), []byte("0123456789abcdef\n"), 0o600)
		},
	} {
		c := fresh()
		set(&c)
		ip := "192.168.1.20"
		if name == "no lan address" {
			ip = ""
		}
		before := c.Gateway.Listen
		if got, err := c.FirstStartLAN(ip); got != "" || err != nil || c.Gateway.Listen != before {
			t.Errorf("%s: %q, %v (listen %q)", name, got, err, c.Gateway.Listen)
		}
	}
	// A data directory that cannot be written keeps the address.
	c = fresh()
	c.DataDir = filepath.Join(c.DataDir, "file")
	_ = os.WriteFile(c.DataDir, nil, 0o600)
	if got, err := c.FirstStartLAN("192.168.1.20"); err == nil || got != "" || c.Gateway.Listen != DefaultListen {
		t.Errorf("unwritable data dir: %q, %v", got, err)
	}
}
