---
kind: feature
upgrade: []
---
**R2** buckets (wrangler `r2_buckets`): the index is a platform store, and the bytes go to a host `fs` or `s3` backend (`-r2-backend`; `twd r2`; ADR-036). Bytes on the `fs` backend stay on their host.
