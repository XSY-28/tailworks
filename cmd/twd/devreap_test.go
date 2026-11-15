package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rosskukulinski/twd/e2e/cliflow"
	"github.com/rosskukulinski/twd/hostd/hostdtest"
	"github.com/rosskukulinski/twd/internal/devreap"
	"github.com/rosskukulinski/twd/netd/nettest"
)

// TestDevSIGKILL is issue #44: `twd dev` killed with SIGKILL (what an IDE,
// coding agent or CI timeout sends) must not leave its workerd running or
// its /tmp/twd-dev-* dir behind.
func TestDevSIGKILL(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	wd, err := nettest.WorkerdBinary()
	if err != nil || !hostdtest.HasNode() {
		t.Skip("needs workerd and node (make deps)")
	}
	bin := cliflow.BuildBinaries(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(filepath.Join(bin, "twd"), "dev", hostdtest.Fixture("probe"), "--port", fmt.Sprint(port))
	cmd.Env = append(os.Environ(), "TWD_WORKERD="+wd)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	// Its dir is the /tmp/twd-dev-* whose owner file names its pid; wait
	// until workerd runs in it.
	var dir string
	var pids []int
	deadline := time.Now().Add(90 * time.Second)
	for len(pids) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("twd dev did not start workerd:\n%s", out.String())
		}
		time.Sleep(250 * time.Millisecond)
		ds, _ := filepath.Glob(devTmpGlob)
		for _, d := range ds {
			var o struct{ PID int }
			b, _ := os.ReadFile(filepath.Join(d, devreap.OwnerFile))
			if json.Unmarshal(b, &o) == nil && o.PID == cmd.Process.Pid {
				dir = d
			}
		}
		if dir != "" {
			pids = devreap.Processes(dir, wd)
		}
	}
	t.Cleanup(func() {
		for _, p := range pids {
			syscall.Kill(p, syscall.SIGKILL)
		}
	})

	cmd.Process.Signal(syscall.SIGKILL)
	cmd.Wait()
	deadline = time.Now().Add(15 * time.Second)
	for {
		alive := 0
		for _, p := range pids {
			if syscall.Kill(p, 0) == nil {
				alive++
			}
		}
		_, err := os.Stat(dir)
		if alive == 0 && os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after SIGKILL: %d of workerd %v still running, %s stat: %v", alive, pids, dir, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
