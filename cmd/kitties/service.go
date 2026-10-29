package main

import (
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
)

// kitties gateway start|stop|restart|status|logs: the systemd service that scripts/install.sh sets up,
// so nobody has to remember systemctl and journalctl. Plain `kitties gateway` still runs it in the
// foreground (that is what the unit itself runs).

// Seams for the tests.
var (
	serviceName = func() string {
		if n := os.Getenv("KITTIES_SERVICE"); n != "" {
			return n
		}
		return "kitties"
	}
	lookPath      = osexec.LookPath
	isRoot        = func() bool { return os.Geteuid() == 0 }
	serviceRunner = func(name string, args []string, in io.Reader, out, errOut io.Writer) error {
		c := osexec.Command(name, args...)
		c.Stdin, c.Stdout, c.Stderr = in, out, errOut
		return c.Run()
	}
)

func isServiceVerb(s string) bool {
	switch s {
	case "start", "stop", "restart", "status", "logs":
		return true
	}
	return false
}

func cmdGatewayService(verb string, args []string, in io.Reader, out, errOut io.Writer) int {
	unit := serviceName()
	var tool string
	var argv []string
	switch verb {
	case "start", "stop", "restart":
		tool, argv = "systemctl", []string{verb, unit}
	case "status":
		tool, argv = "systemctl", []string{"status", "--no-pager", unit}
	case "logs":
		// Whatever follows goes to journalctl; by default the last lines, followed.
		tool, argv = "journalctl", []string{"-u", unit}
		if len(args) == 0 {
			args = []string{"-n", "100", "-f"}
		}
		argv = append(argv, args...)
	}
	if _, err := lookPath(tool); err != nil {
		fmt.Fprintf(errOut, "kitties: %s not found: gateway %s manages the systemd service set up by scripts/install.sh; "+
			"without it, run `kitties gateway` in the foreground\n", tool, verb)
		return 1
	}
	// Changing the service needs root; asking for it is friendlier than failing.
	if verb != "status" && verb != "logs" && !isRoot() {
		if _, err := lookPath("sudo"); err == nil {
			tool, argv = "sudo", append([]string{"systemctl"}, argv...)
		}
	}
	if err := serviceRunner(tool, argv, in, out, errOut); err != nil {
		fmt.Fprintf(errOut, "kitties: `%s %s` failed: %v\n", tool, strings.Join(argv, " "), err)
		if verb == "status" {
			return 3 // systemctl status: not running
		}
		return 1
	}
	switch verb {
	case "start", "restart":
		fmt.Fprintf(out, "kitties: gateway %sed; logs: kitties gateway logs\n", verb)
	case "stop":
		fmt.Fprintln(out, "kitties: gateway stopped")
	}
	return 0
}
