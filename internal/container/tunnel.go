package container

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// THE TUNNEL.
//
// The desktop's live view is voxl (https://github.com/tembo/voxl): an H.264 stream over a
// WebSocket that the image runs on the container's own loopback. The container publishes no port,
// so the gateway reaches it the way it reaches everything else: `docker exec -i <name> socat`,
// whose standard input and output are one TCP connection to 127.0.0.1:<port> inside. The
// connection has TCP_NODELAY (socat's `nodelay`): input events are a few bytes each and must not
// wait to be joined to the next ones.

// VoxlPort is where the image runs voxl, on the container's loopback.
const VoxlPort = 6901

// startTunnel runs the command that joins its standard input and output to a TCP connection in
// the container; a test replaces it.
var startTunnel = func(ctx context.Context, bin string, args ...string) (io.WriteCloser, io.ReadCloser, func() error, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	return in, out, cmd.Wait, nil
}

// directTimeout is how long the direct connection gets before the tunnel is used instead.
const directTimeout = 500 * time.Millisecond

// addrTTL is how long a container's address is remembered.
const addrTTL = 30 * time.Second

type addrEntry struct {
	ip      string
	expires time.Time
}

var addrCache sync.Map // container name -> addrEntry

// containerIP is the container's address on the engine's own network ("" when it has none or
// the engine does not say), looked up with `inspect` and remembered a little while.
func (m *Manager) containerIP(ctx context.Context, name string) string {
	if e, ok := addrCache.Load(name); ok && time.Now().Before(e.(addrEntry).expires) {
		return e.(addrEntry).ip
	}
	ip := ""
	out, err := runCmd(ctx, m.rt.Bin, "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", name)
	if err == nil {
		for _, f := range strings.Fields(string(out)) {
			if a, perr := netip.ParseAddr(f); perr == nil && a.Is4() {
				ip = f
				break
			}
		}
	}
	addrCache.Store(name, addrEntry{ip: ip, expires: time.Now().Add(addrTTL)})
	return ip
}

// Tunnel opens a TCP connection to port inside the container. It first tries the container's
// own address on the engine's network (the image's voxl listens there; nothing is published to
// the host): no `docker exec`, no engine in the middle of every packet. Where that cannot be
// reached (Docker Desktop, a remote engine) it is 127.0.0.1:port inside the container through
// `docker exec socat`. Closing it ends the connection (and the command in the container).
func (m *Manager) Tunnel(ctx context.Context, name string, port int) (net.Conn, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("container name %q is not valid", name)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("port %d is not valid", port)
	}
	if nameRe.MatchString(name) {
		if ip := m.containerIP(ctx, name); ip != "" {
			d := net.Dialer{Timeout: directTimeout}
			if c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port))); err == nil {
				return c, nil
			}
			// Not reachable from here (Docker Desktop, a remote engine): the tunnel for a while.
			addrCache.Store(name, addrEntry{expires: time.Now().Add(addrTTL)})
		}
	}
	// The command outlives the dial's context (the connection is the caller's); Close ends it.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	in, out, wait, err := startTunnel(runCtx, m.rt.Bin, "exec", "-i", name, "socat", "-", "TCP:127.0.0.1:"+strconv.Itoa(port)+",nodelay")
	if err != nil {
		cancel()
		return nil, err
	}
	return &tunnel{in: in, out: out, wait: wait, cancel: cancel}, nil
}

// tunnel is the connection: what is written goes to the container's input, what is read comes
// from its output.
type tunnel struct {
	in     io.WriteCloser
	out    io.ReadCloser
	wait   func() error
	cancel context.CancelFunc
	once   sync.Once
}

func (t *tunnel) Read(p []byte) (int, error)  { return t.out.Read(p) }
func (t *tunnel) Write(p []byte) (int, error) { return t.in.Write(p) }

// Close ends the connection: the input closes first (socat ends on it), then docker is stopped.
func (t *tunnel) Close() error {
	t.once.Do(func() {
		_ = t.in.Close()
		t.cancel()
		_ = t.out.Close()
		_ = t.wait()
	})
	return nil
}

type tunnelAddr struct{}

func (tunnelAddr) Network() string { return "docker-exec" }
func (tunnelAddr) String() string  { return "docker-exec" }

func (t *tunnel) LocalAddr() net.Addr              { return tunnelAddr{} }
func (t *tunnel) RemoteAddr() net.Addr             { return tunnelAddr{} }
func (t *tunnel) SetDeadline(time.Time) error      { return nil }
func (t *tunnel) SetReadDeadline(time.Time) error  { return nil }
func (t *tunnel) SetWriteDeadline(time.Time) error { return nil }
