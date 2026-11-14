package netd_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/rosskukulinski/twd/netd"
)

// TestEgressWaitsForNewPeerPath: object a calls a node that has just
// registered, as soon as it is in a's netmap (before its first netcheck, so
// before it has a home DERP or endpoints). Without awaitPeerPath the first
// handshake initiation has no path, is dropped, and WireGuard retries after
// 5 s; with it the call completes as soon as the path exists.
func TestEgressWaitsForNewPeerPath(t *testing.T) {
	ctl := startControl(t)
	mint := &countingMint{}
	regA, err := netd.New(netd.Config{ControlURL: ctl.URL, StateRoot: filepath.Join(newRundir(t), "a"), Keys: mint.source()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { regA.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a, err := regA.Create("a", netd.ObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	lcA, err := a.Server().LocalClient()
	if err != nil {
		t.Fatal(err)
	}
	lcA.OmitAuth = true

	// call creates a fresh node on another registry (not a sibling) and
	// GETs it through a's egress the moment it appears in a's netmap.
	call := func(id string) time.Duration {
		regB, err := netd.New(netd.Config{ControlURL: ctl.URL, StateRoot: filepath.Join(newRundir(t), id), Keys: mint.source(),
			Upstream: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })})
		if err != nil {
			t.Fatal(err)
		}
		defer regB.Close()
		if _, err := regB.Create(id, netd.ObjectOptions{}); err != nil {
			t.Fatal(err)
		}
		var ip string
		for ip == "" {
			st, err := lcA.Status(ctx)
			if err == nil {
				for _, p := range st.Peer {
					if p.HostName == "twd-"+id && len(p.TailscaleIPs) > 0 {
						ip = p.TailscaleIPs[0].String()
					}
				}
			}
			if ctx.Err() != nil {
				t.Fatal(ctx.Err())
			}
			time.Sleep(5 * time.Millisecond)
		}
		t0 := time.Now()
		c, err := regA.DialEgress(ctx, "a", ip+":80")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", ip)
		c.SetReadDeadline(time.Now().Add(30 * time.Second))
		resp, err := http.ReadResponse(bufio.NewReader(c), nil)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d", id, resp.StatusCode)
		}
		return time.Since(t0)
	}

	old := netd.PeerPathWait
	netd.PeerPathWait = 0
	without := call("b0")
	netd.PeerPathWait = old
	with := call("b1")
	t.Logf("first request to a just-registered peer: without the path wait %v, with it %v", without.Round(time.Millisecond), with.Round(time.Millisecond))
	if with > 2*time.Second {
		t.Errorf("with the path wait the first request took %v; want well under WireGuard's 5 s retry", with)
	}
}
