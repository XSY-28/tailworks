Last update: 2026-11-25 11:40 PT. hostd: the syncService data race (#586), the whois helper panic (#411), the alarm-record flake (#410).

# fix-hostd-flakes

## #586 data race: awaitNode vs syncService

`syncService` read `wl.Status` for its reason string ("workload node …")
after it released `h.mu`, while `awaitNode` writes it under `h.mu`. It now
copies the field while the lock is held. `wl.Hostname`, the only other
unlocked read there, is set once when the object is created.

## #411 serveWhoisTCP nil pointer (connect_test.go:42)

6eee8fcf already handled the nil `who.Node`. The later CI panic (job
139081103575, `addr=0x20`) was on the same line, at
`c.RemoteAddr().String()`. gVisor's `gonet.TCPConn.RemoteAddr` returns nil
once the endpoint is reset (`GetRemoteAddress` fails), and calling
`String` on a nil `net.Addr` reads offset 0x20 of a nil itab. The helper,
and its copy in `e2e/vm/tcp_test.go` (which also lacked the `who.Node`
check), now drop such a conn.

## #410 TestTelemetry/EntryTraces/*Alarm

Reproduced 1 time in 80 runs under load. The `__tw_alarm` record had its
trace but `Logs:[]`, and an `alarmSeen` RPC record from the same
millisecond carried "alarm Obj". The test polled the object (`alarmSeen`)
every 50 ms while the alarm ran, and workerd's tail put the alarm's
console.log on that concurrent invocation. hostd's join wasn't the
problem: the trace was right. The test now waits for the alarm's record
first, and only then reads what the alarm saw. That's 0 failures in
180 runs under the same load.

Note for users: in workerd, a Durable Object's console.log can show up on
a different invocation's record when requests to that object overlap.
This is workerd behaviour, not something tailworks adds.

## Tests

See the PR body.
