package main

import (
	"context"
	"errors"

	"tailscale.com/tsnet"
)

// pushCallerTags returns a push-API caller's tags by WhoIs on the host's
// own node (hostd.Config.PushCallerTags).
func pushCallerTags(ts *tsnet.Server) func(ctx context.Context, remoteAddr string) ([]string, error) {
	return func(ctx context.Context, remoteAddr string) ([]string, error) {
		lc, err := ts.LocalClient()
		if err != nil {
			return nil, err
		}
		who, err := lc.WhoIs(ctx, remoteAddr)
		if err != nil {
			return nil, err
		}
		if who.Node == nil {
			return nil, errors.New("whois: no node")
		}
		return who.Node.Tags, nil
	}
}
