package config

import "testing"

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
