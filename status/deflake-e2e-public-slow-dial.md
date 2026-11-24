# deflake-e2e-public-slow-dial

The v0.5.0 release run (45346852696) failed its `tailnet` job in
`make e2e-public`. Every check passed, then the `/slow` stream request
failed with `dial tcp [<IPv6>]:443: connect: network is unreachable`: the
lookup returned only the Funnel edge's IPv6 address, and GitHub's hosted
runners have no IPv6. That request was the only public one not made through
`fetch`, which retries a failure to resolve or connect for a minute. It now
retries the same way. The job passed when rerun.

## Tests

- `make lint`: ok (macOS arm64, Go 1.27.4).
- `go vet -tags e2epublic ./e2e/public/`: ok.
- Not run: `make e2e-public` (needs the test tailnet's credentials and
  Funnel), and `make test` (the only change is in a file behind the
  `e2epublic` build tag, which `make test` doesn't build).
