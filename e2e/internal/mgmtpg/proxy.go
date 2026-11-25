package mgmtpg

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Proxy sits between one replica and Postgres and injects faults:
// pass, delay (each forwarded read waits d, both directions), blackhole
// (accept and hold connections, forward nothing) and reset (close every
// connection now; new ones are refused while the mode lasts).
type Proxy struct {
	ln     net.Listener
	target string
	mode   atomic.Value // string
	delay  atomic.Int64 // ns
	mu     sync.Mutex
	conns  map[net.Conn]bool
}

// NewProxy listens on a free 127.0.0.1 port and forwards to target
// (host:port). It closes when the test ends.
func NewProxy(t testing.TB, target string) *Proxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{ln: ln, target: target, conns: map[net.Conn]bool{}}
	p.mode.Store("pass")
	go p.serve()
	t.Cleanup(func() { ln.Close(); p.CloseAll() })
	return p
}

// Addr is the proxy's host:port.
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// Set changes the mode; "reset" also drops every open connection.
func (p *Proxy) Set(mode string, d time.Duration) {
	p.delay.Store(int64(d))
	p.mode.Store(mode)
	if mode == "reset" {
		p.CloseAll()
	}
}

// Mode is the current mode.
func (p *Proxy) Mode() string { return p.mode.Load().(string) }

// CloseAll drops every open connection.
func (p *Proxy) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		c.Close()
		delete(p.conns, c)
	}
}

func (p *Proxy) track(c net.Conn) {
	p.mu.Lock()
	p.conns[c] = true
	p.mu.Unlock()
}

func (p *Proxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		if p.mode.Load() == "reset" {
			c.Close()
			continue
		}
		up, err := net.Dial("tcp", p.target)
		if err != nil {
			c.Close()
			continue
		}
		p.track(c)
		p.track(up)
		go p.pipe(up, c)
		go p.pipe(c, up)
	}
}

func (p *Proxy) pipe(dst, src net.Conn) {
	defer dst.Close()
	defer src.Close()
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			for p.mode.Load() == "blackhole" {
				time.Sleep(50 * time.Millisecond)
			}
			if d := time.Duration(p.delay.Load()); p.mode.Load() == "delay" && d > 0 {
				time.Sleep(d)
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
