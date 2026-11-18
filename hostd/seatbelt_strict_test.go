package hostd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rosskukulinski/tailworks/netd/nettest"
)

// The strict profile's shape (issue #121): global denies first, each
// allow after the deny it narrows (Seatbelt: the last match wins).
func TestSeatbeltStrictProfileShape(t *testing.T) {
	dir := realPath(t.TempDir())
	p := seatbeltProfile([]string{dir + "/run/i1"}, []string{"/opt/workerd"}, nil, []string{dir + "/data"},
		[]uint16{4001}, []uint16{4001, 4002})
	lines := strings.Split(p, "\n")
	at := func(s string) int {
		for i, l := range lines {
			if l == s {
				return i
			}
		}
		t.Fatalf("profile lacks %q:\n%s", s, p)
		return -1
	}
	order := [][2]string{
		{`(deny file-write*)`, `(allow file-read* file-write* (subpath "` + dir + `/run/i1"))`},
		{`(deny file-write*)`, `(allow file-write* (literal "/dev/null"))`},
		{`(deny network*)`, `(allow network-outbound (remote tcp "localhost:4002"))`},
		{`(deny network*)`, `(allow network-bind network-inbound (local tcp "localhost:4001"))`},
		{`(deny network*)`, `(allow network-outbound (remote unix-socket (subpath "` + dir + `/run/i1")))`},
		{`(deny signal)`, `(allow signal (target same-sandbox))`},
		{`(deny process-exec*)`, `(allow process-exec (literal "/opt/workerd"))`},
		{`(deny file-read* file-write* (subpath "` + dir + `/data"))`, `(allow file-read* (literal "/opt/workerd"))`},
	}
	for _, o := range order {
		if at(o[1]) < at(o[0]) {
			t.Errorf("%q must follow %q", o[1], o[0])
		}
	}
	if strings.Contains(p, `(remote tcp "*`) || strings.Contains(p, "localhost:4003") {
		t.Fatalf("unexpected TCP allow:\n%s", p)
	}
}

// Under the strict profile, the probes that succeeded under the old
// (allow default) profile fail: a TCP connect to a port it wasn't given, a
// signal to another process, a write outside its dirs, a TCP bind of a
// port it wasn't given, an exec of a binary outside the allowed dirs. What
// a workload needs still works: its own dir, its own unix sockets, its TCP
// ports, signalling itself and its children, /bin/sh, workerd and node.
func TestSeatbeltStrictProbes(t *testing.T) {
	if _, err := ResolveSandbox(SandboxSeatbelt); err != nil {
		t.Skip(err)
	}
	tmp := realPath(t.TempDir())
	own, deny, other := filepath.Join(tmp, "own"), filepath.Join(tmp, "deny"), filepath.Join(tmp, "other")
	for _, d := range []string{own, deny, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	listen := func() (net.Listener, uint16) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
		return ln, uint16(ln.Addr().(*net.TCPAddr).Port)
	}
	_, allowed := listen()   // the egress proxy
	_, forbidden := listen() // another tenant's port, hostd's, …
	bindOK, bindNo := freePort(t), freePort(t)
	// A unix socket outside its dirs (say, tailscaled's); short path.
	sdir, err := os.MkdirTemp("/tmp", "twsb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sdir)
	sock := filepath.Join(realPath(sdir), "s.sock")
	uln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer uln.Close()
	go func() {
		for {
			c, err := uln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	echo := filepath.Join(other, "echo")
	ownEcho := filepath.Join(own, "echo")
	for _, p := range []string{echo, ownEcho} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\necho hi\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(p).CombinedOutput(); err != nil {
			t.Fatalf("%s unsandboxed: %v %s", p, err, out)
		}
	}
	readOnly := []string{}
	wd, wdErr := nettest.WorkerdBinary()
	if wdErr == nil {
		readOnly = append(readOnly, wd)
	}
	nodeBin, nodeErr := exec.LookPath("node")
	var trees []string
	if nodeErr == nil {
		readOnly = append(readOnly, nodeBin)
		trees = nodePrefix(nodeBin)
	}
	home, _ := os.UserHomeDir()
	prof := filepath.Join(own, "sandbox.sb")
	text := seatbeltProfile([]string{own}, readOnly, trees, []string{deny, home}, []uint16{bindOK}, []uint16{allowed, bindOK})
	if err := os.WriteFile(prof, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, sandboxExec, append([]string{"-f", prof}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	// The probes work unsandboxed, so a failure below is the profile's.
	victim := exec.Command("/bin/sleep", "60")
	if err := victim.Start(); err != nil {
		t.Fatal(err)
	}
	defer victim.Process.Kill()
	tmpFile := fmt.Sprintf("/tmp/tw-seatbelt-probe-%d", os.Getpid())
	defer os.Remove(tmpFile)
	nc := func(port uint16) []string {
		return []string{"/usr/bin/nc", "-z", "-w", "3", "127.0.0.1", strconv.Itoa(int(port))}
	}
	bind := func(port uint16) []string {
		return []string{"/usr/bin/perl", "-MIO::Socket::INET", "-e",
			`IO::Socket::INET->new(LocalAddr => "127.0.0.1", LocalPort => $ARGV[0], Listen => 1, ReuseAddr => 1) or die "bind: $!\n"`,
			strconv.Itoa(int(port))}
	}
	denied := map[string][]string{
		"tcp_connect_other_port": nc(forbidden),
		"tcp_connect_internet":   {"/usr/bin/nc", "-z", "-w", "3", "1.1.1.1", "443"},
		"tcp_bind_other_port":    bind(bindNo),
		"signal_other_process":   {"/bin/kill", "-0", strconv.Itoa(victim.Process.Pid)},
		"signal_hostd":           {"/bin/kill", "-0", strconv.Itoa(os.Getpid())},
		"write_outside_own":      {"/bin/sh", "-c", "echo x > " + filepath.Join(other, "f")},
		"write_tmp":              {"/bin/sh", "-c", "echo x > " + tmpFile},
		"read_denied":            {"/bin/ls", deny},
		"exec_outside_allowed":   {echo, "hi"},
		"unix_connect_not_own":   {"/usr/bin/perl", "-MIO::Socket::UNIX", "-e", `IO::Socket::UNIX->new(Peer => $ARGV[0]) or die "$!\n"`, sock},
	}
	for name, args := range denied {
		if name != "tcp_connect_internet" { // offline CI hosts
			if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
				t.Fatalf("%s fails unsandboxed too, so it proves nothing: %v %s", name, err, out)
			}
			os.Remove(tmpFile)
			os.Remove(filepath.Join(other, "f"))
		}
		if out, err := run(args...); err == nil {
			t.Errorf("%s: allowed under the strict profile (%v): %s", name, args, out)
		}
	}
	allowedProbes := map[string][]string{
		"tcp_connect_given_port": nc(allowed),
		"tcp_bind_own_port":      bind(bindOK),
		"signal_self":            {"/bin/sh", "-c", "kill -0 $$"},
		"signal_child":           {"/bin/sh", "-c", "/bin/sleep 30 & kill $!"},
		"write_own":              {"/bin/sh", "-c", "echo x > " + filepath.Join(own, "f") + " && cat " + filepath.Join(own, "f")},
		"write_dev_null":         {"/bin/sh", "-c", "echo x > /dev/null"},
		"exec_own":               {ownEcho, "hi"},
		"unix_socket_own":        {"/usr/bin/perl", "-MIO::Socket::UNIX", "-e", `IO::Socket::UNIX->new(Local => $ARGV[0], Listen => 1) or die "$!\n"`, filepath.Join(own, "s.sock")},
	}
	if wdErr == nil {
		allowedProbes["workerd"] = []string{wd, "--version"}
	}
	if nodeErr == nil {
		// node listens on its own port and connects to the egress proxy.
		allowedProbes["node"] = []string{nodeBin, "-e", fmt.Sprintf(`const net = require("net");
const srv = net.createServer((c) => c.end()).listen(%d, "127.0.0.1", () => {
  const c = net.connect(%d, "127.0.0.1", () => { c.end(); srv.close(); });
  c.on("error", (e) => { console.error(e); process.exit(1); });
});
srv.on("error", (e) => { console.error(e); process.exit(1); });`, bindOK, allowed)}
	}
	t.Logf("workerd: %v; node: %v", wd, nodeBin)
	for name, args := range allowedProbes {
		if out, err := run(args...); err != nil {
			t.Errorf("%s: denied under the strict profile (%v): %v %s", name, args, err, out)
		}
	}
	if _, err := os.Stat(tmpFile); err == nil {
		t.Errorf("%s was written from the sandbox", tmpFile)
	}
	if victim.ProcessState != nil {
		t.Error("the victim process exited")
	}
}

func freePort(t *testing.T) uint16 {
	t.Helper()
	p, err := freeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	return uint16(p)
}
