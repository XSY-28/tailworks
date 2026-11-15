---
kind: fix
upgrade: []
---
`twd dev` no longer leaves workerd running and `/tmp/twd-dev-*` behind when it is killed with SIGKILL (an IDE, a coding agent or a CI timeout). A small watchdog stops its workerd and node processes and removes the dir however `twd dev` exits, and the next `twd dev` cleans up after any earlier run whose process is gone. Another `twd dev` that is still running is left alone. The hostd and netd test helpers do the same for their workerd processes (#44).
