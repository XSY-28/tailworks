---
kind: fix
upgrade: [workerd]
---
The pinned workerd moves from 1.20261115.1 to 1.20261118.1. It brings
workerd's fixes for nested dynamic imports (cloudflare/workerd#8517, #8541),
keeps a `DigestStream` alive while its sink runs (#8533), fixes several Rust
JSG garbage-collection issues, and adds `"brotli"` to `CompressionStream` and
`DecompressionStream` (#8483). Apps don't need changes.

Operator action: workerd moves to 1.20261118.1.
