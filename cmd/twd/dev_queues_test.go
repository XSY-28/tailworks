package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rosskukulinski/twd/e2e/cliflow"
	"github.com/rosskukulinski/twd/hostd/hostdtest"
	"github.com/rosskukulinski/twd/netd/nettest"
)

// TestDevQueues: `twd dev` runs a Worker's queue producers and consumers
// locally (ADR-039): a send through env.JOBS on the dev port reaches
// the app's own queue() handler, through the dev's fake mgmt.
func TestDevQueues(t *testing.T) {
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
	work := t.TempDir()
	cmd := exec.Command(filepath.Join(bin, "twd"), "dev", hostdtest.Fixture("queues"), "--port", fmt.Sprint(port), "--state-dir", filepath.Join(work, "state"))
	cmd.Env = append(os.Environ(), "TWD_WORKERD="+wd)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			cmd.Process.Kill()
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	get := func(path string) (int, string) {
		r, err := http.Get(base + path)
		if err != nil {
			return 0, err.Error()
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		if code, _ := get("/env"); code == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("twd dev did not serve the app:\n%s", out.String())
		}
		time.Sleep(250 * time.Millisecond)
	}
	if code, body := get("/send?k=dev1"); code != 200 {
		t.Fatalf("send: %d %s", code, body)
	}
	deadline = time.Now().Add(30 * time.Second)
	for {
		_, body := get("/log")
		var l struct{ Recs []struct{ K string } }
		json.Unmarshal([]byte(body), &l)
		if len(l.Recs) > 0 && l.Recs[0].K == "dev1" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the consumer never ran under twd dev: %s\n%s", body, out.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
}
