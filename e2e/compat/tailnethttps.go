package compat

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// deployedBase is where the deployed run calls p: http://<workload IP>,
// or https://<MagicDNS name> for a TailnetHTTPS project (#360), whose
// certificate is for that name.
func deployedBase(p *Project, ip, magicDNS string) string {
	if p.TailnetHTTPS {
		return "https://" + strings.TrimSuffix(magicDNS, ".")
	}
	return "http://" + ip
}

// deployedClient is the probe's HTTP client: every connection goes to the
// workload's IP over the tailnet (dial), on the URL's port, so an
// https://<name> URL needs no MagicDNS lookup on the client.
func deployedClient(dial func(ctx context.Context, network, addr string) (net.Conn, error), ip string, timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return dial(ctx, network, net.JoinHostPort(ip, port))
		},
		Proxy:               nil,
		MaxIdleConnsPerHost: 8,
	}}
}

// tailnetHTTPSCertWait bounds waitTailnetCert: a new node name's first
// ts.net certificate takes 30–100 s, sometimes minutes (netd/certprefetch.go).
const tailnetHTTPSCertWait = 5 * time.Minute

// waitTailnetCert waits until a TLS handshake with the workload's :443
// listener completes for name, i.e. its certificate has been issued, so
// the probe's 60 s request timeout measures the app and not certificate
// issuance (#614). It returns how long that took.
func waitTailnetCert(ctx context.Context, dial func(ctx context.Context, network, addr string) (net.Conn, error), ip, name string) (time.Duration, error) {
	name = strings.TrimSuffix(name, ".")
	st := time.Now()
	ctx, cancel := context.WithTimeout(ctx, tailnetHTTPSCertWait)
	defer cancel()
	var last error
	for {
		actx, acancel := context.WithTimeout(ctx, 30*time.Second)
		c, err := dial(actx, "tcp", net.JoinHostPort(ip, "443"))
		if err == nil {
			tc := tls.Client(c, &tls.Config{ServerName: name})
			err = tc.HandshakeContext(actx)
			tc.Close()
		}
		acancel()
		if err == nil {
			return time.Since(st), nil
		}
		last = err
		select {
		case <-ctx.Done():
			return time.Since(st), fmt.Errorf("no TLS handshake with %s within %s: %v", name, tailnetHTTPSCertWait, last)
		case <-time.After(2 * time.Second):
		}
	}
}
