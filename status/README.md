# Build status, one file per workstream

`STATUS.md` at the repo root is the frozen history up to 2026-11-14. New
status goes here, **one file per branch / workstream**: `status/<branch>.md`
(for example `status/workload-replicas.md`).

Why: `STATUS.md` was one 300 KB log that every branch edited, at the same
spots (a new `Last update:` line at the top, a new section at the end). It
conflicted in 10 of the last 11 merges. Separate files never collide.

Rules:

- Write only your own branch's file. Don't edit another workstream's file,
  and don't add to `STATUS.md`.
- Put the date in your file, not in a shared header: start with
  `Last update: YYYY-MM-DD HH:MM PT. <one line>` and keep the newest at the
  top of *your* file.
- Same content as before: what's built, test results with the commands that
  produced them, findings and decisions, known gaps and next steps.
- To refer to older results, cite `STATUS.md` and the section name
  (for example `STATUS.md "Fast identity"`).
- Keep the file after merge. It's the record. A later workstream that
  continues the work gets its own file and links back.

To see what changed recently: `ls -t status/` or
`git log --format='%h %ad %s' --date=short -- status/`.
