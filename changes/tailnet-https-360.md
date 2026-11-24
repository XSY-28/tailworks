---
kind: feature
upgrade: [feature-string]
---
A workload can now be served over HTTPS on the tailnet: set
`"tailnet": {"https": true}` in tailworks.jsonc and the workload node also
listens on `https://<name>.<tailnet>.ts.net` (port 443) with its own ts.net
certificate. A request that arrives that way reaches the Worker with an
`https://` `request.url` and `X-Forwarded-Proto: https` (a Node-tier app
gets the header), so code that insists on an HTTPS origin runs unmodified:
`@cloudflare/workers-oauth-provider`'s resource URL, `Secure` cookies,
OAuth redirect URIs (#360). `http://` on port 80 keeps working, unchanged
and not redirected; `tw deploy` prints the `https://` URL.

The tailnet needs HTTPS certificates (and MagicDNS). Without them the
deploy fails with `tailnet_https_unavailable` and the active deployment
keeps serving. Headscale issues no ts.net certificates. Only the workload
node gets the listener: objects' own nodes and Tailscale Service names stay
`http://`. `tw dev` serves its local `http://` URL only and says so.

Operator action: a bundle with `tailnet.https` is placed only on hosts that
offer the `tailnet-https` feature; upgrade the hosts first. Callers need a
grant to the workload's tags on `tcp:443` as well as `tcp:80`; the caller
grant tailworks prints and writes covers `tcp:80` only.
