# deploy-image-pins (#522)

`tw deploy` shows the container image pins mgmt resolved at deploy: one
`✓ image` line per pin of a ref it did not build (built images already get
their line from the build step), after the deployment is ready, and `images[]`
in the `--json` result and on the `ready` event (not `placed`: pins are not in
the deploy response). Display only; nothing is written back to
`tailworks.jsonc` (per the issue's verifier amendments). No line under
`--no-wait` (it stays a single POST).

The line is printed from the final deployment view, after the shared poll
loop, rather than inside it, so it does not collide with PR #617 (which moves
that loop into `waitReady`). Pins are fixed at deploy, so the first and last
poll carry the same pins.

Not done: appending `from` to `tw ls`/`tw status`'s image sub-line (optional in
the issue; D4 alignment).

## Tests
