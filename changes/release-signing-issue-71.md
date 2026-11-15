---
kind: security
upgrade: []
---
Releases are signed. Each release's `SHA256SUMS`, and each tarball's, gets a `SHA256SUMS.sig`: an SSH signature (`ssh-keygen -Y`) by a twd release key that is pinned in `scripts/get.sh`, `install.sh` and the twd binaries. `get.sh` checks the signature before it trusts the checksums, and `install.sh` checks it before it installs. twd-hostd's self-upgrade checks it in Go against the keys built into the running binary, so a compromised twd-mgmt can no longer push a release of its own. An unsigned or badly signed release is refused. `install.sh --allow-unsigned` (or `TWD_ALLOW_UNSIGNED=1`) installs a build you made yourself, and local builds (`x.y.z-<commit>`) install with a warning. `get.sh` still installs releases from before v0.3.0, with a warning, on their checksum alone. To check a release by hand, see INSTALL.md, "Verify a release" (#71).

Operator action: a host that self-upgrades now refuses releases older than v0.3.0, which are unsigned. Upgrade such hosts by hand.
