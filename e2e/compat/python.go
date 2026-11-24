package compat

// Cloudflare's Python Workers examples (cloudflare/python-workers-examples,
// issue #292), published in COMPAT.md's "python-workers" table. The ones
// that need no package vendored by pywrangler run unmodified; the rest are
// in pythonNotRun with the reason.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// pythonExamplesCommit is cloudflare/python-workers-examples main.
const pythonExamplesCommit = "47ec7d69f4052cd57bed5d3676cb7af76ca0656b" // 2026-11-24

// SectionPython is the COMPAT.md table of the Python examples.
const SectionPython = "python-workers"

func pyEx(id, dir string, f func(*Project)) *Project {
	p := Project{ID: id, Repo: "cloudflare/python-workers-examples", Commit: pythonExamplesCommit, Dir: dir,
		Section: SectionPython, Install: InstallNone, Exercises: []string{"python"}}
	f(&p)
	return &p
}

// pyRefused is an example tw refuses, naming why.
func pyRefused(id, dir, what, err, why string) *Project {
	return pyEx(id, dir, func(p *Project) {
		p.What = what
		p.Expect = Expect{Kind: ExpectRefused, Compat: "python", Error: err, Why: why}
	})
}

func pythonProjects() []*Project {
	return []*Project{
		pyEx("py-hello", "hello", func(p *Project) {
			p.What = "Python Worker: fetch handler"
			p.Probe = func(r *Run) error { return r.Get("/").Want(200, "Hello world!") }
		}),
		pyEx("py-binding", "binding", func(p *Project) {
			p.What = "Python Worker: KV put and get"
			p.Exercises = []string{"python", "kv"}
			p.Files = map[string]string{"wrangler.jsonc": `{
	"name": "hello-python-bindings",
	"main": "src/entry.py",
	"compatibility_date": "2025-11-02",
	"compatibility_flags": ["python_workers"],
	"kv_namespaces": [{"binding": "FOO", "id": "foo"}],
	"observability": {"enabled": true}
}
`}
			p.Workarounds = []string{"the config's `<YOUR_KV_NAMESPACE_ID>` placeholder is set to a namespace name, as a user sets it to the ID of the namespace they created"}
			p.Probe = func(r *Run) error { return r.Get("/").Want(200, "baz") }
		}),
		pyEx("py-durable-objects", "durable-objects", func(p *Project) {
			p.What = "Python Durable Object: RPC methods, storage"
			p.Exercises = []string{"python", "do-rpc", "do-storage"}
			p.Probe = func(r *Run) error {
				if err := r.Get("/l1/add/hello").Want(200, "Message sent"); err != nil {
					return err
				}
				if err := r.Get("/l1/add/again").Want(200, "Message sent"); err != nil {
					return err
				}
				if err := r.Get("/l1/show").Want(200, "hello\nagain"); err != nil {
					return err
				}
				return r.Get("/l2/show").Want(200, "No messages")
			}
		}),
		pyEx("py-chatroom", "chatroom", func(p *Project) {
			p.What = "Python Durable Object: hibernatable WebSocket chatroom, an HTML file read from disk"
			p.Exercises = []string{"python", "websockets", "hibernation"}
			p.Probe = pyChatroomProbe
		}),
		pyEx("py-opengraph", "opengraph", func(p *Project) {
			p.What = "Python Worker: HTMLRewriter over an internet fetch"
			p.Exercises = []string{"python", "fetch"}
			p.Probe = func(r *Run) error { return r.Get("/").Want(200, `og:title`, "Example Domain") }
		}),
		pyRefused("py-cron", "cron", "Python Worker: cron trigger", "triggers.crons: not supported for Python Workers",
			"cron triggers of Python Workers aren't supported yet (docs/python.md#limits)"),
		pyRefused("py-workflows", "workflows", "Python Workflow", "workflows: not supported for Python Workers",
			"Python Workflows aren't supported yet (docs/python.md#limits)"),
		pyRefused("py-assets", "assets", "Python Worker + static assets", "assets: not supported for Python Workers",
			"static assets in front of a Python Worker aren't supported yet (docs/python.md#limits)"),
		pyRefused("py-url-shortener", "url-shortener", "Python Worker on KV, compatibility_date 2026-09-01",
			"no longer has the `workers` SDK built in",
			"from compatibility_date 2026-04-21 the `workers` SDK is a package pywrangler vendors (workers-runtime-sdk); tw deploy doesn't run pywrangler, so it asks for `uv run pywrangler sync` first (docs/python.md#packages)"),
	}
}

// pyChatroomProbe: the page, then two clients in a room: one's message
// reaches the other.
func pyChatroomProbe(r *Run) error {
	if err := r.Get("/").Want(200, "<title>Chatroom</title>"); err != nil {
		return err
	}
	a, err := r.WS("/room/compat")
	if err != nil {
		return err
	}
	defer a.CloseNow()
	if m, err := WSRead(a, 10*time.Second); err != nil || !strings.Contains(m, "Connected to chatroom") {
		return fmt.Errorf("first client's welcome: %q %v", m, err)
	}
	b, err := r.WS("/room/compat")
	if err != nil {
		return err
	}
	defer b.CloseNow()
	WSRead(b, 10*time.Second) // its welcome
	msg, _ := json.Marshal(map[string]string{"username": "compat", "text": "hello from the tailnet"})
	if err := WSWrite(b, string(msg)); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m, err := WSRead(a, time.Until(deadline))
		if err != nil {
			return fmt.Errorf("the first client got no broadcast: %v", err)
		}
		if strings.Contains(m, "hello from the tailnet") {
			r.Note("broadcast %q", tail(m, 200))
			return nil
		}
	}
	return fmt.Errorf("the first client got no broadcast")
}

// pythonNotRun are the Python examples without a Project.
func pythonNotRun() []NotRun {
	py := func(why string, dirs ...string) NotRun {
		return NotRun{Section: SectionPython, Repo: "cloudflare/python-workers-examples", Commit: pythonExamplesCommit, Dirs: dirs, Why: why}
	}
	return []NotRun{
		py("needs packages vendored by pywrangler (`uv run pywrangler sync` into python_modules/); tw deploy ships what is vendored, and the suite doesn't run uv yet",
			"django", "django-markdown-r2", "django-todo-d1", "fastapi", "fastapi-todo", "flask-todo", "image-gen", "js-api-pygments", "mcp-server", "sync-http-clients"),
		py("needs Workers AI (an `ai` binding), which tailworks doesn't provide",
			"image-redraw", "langchain", "vectorize-rag", "workers-ai"),
		py("needs Hyperdrive", "hyperdrive-batched-counter"),
		py("needs the Worker Loader (`worker_loaders` binding), which tailworks keeps to the platform", "dynamic-py-py"),
		py("needs its database seeded after the deploy (`d1 execute --file db_init.sql`), which `tw dev` runs can't do; run by `make e2e-compat` as `py-query-d1`", "query-d1"),
		py("a long-running Durable Object on the public Bluesky firehose (an outbound WebSocket); not tried", "websocket-stream-consumer"),
		py("the repo's test harness, not an example", "tests"),
	}
}

// pythonDeployedOnly are Python examples only the deployed run can
// prepare (post-deploy steps), outside the published table.
func pythonDeployedOnly() []*Project {
	return []*Project{
		{ID: "py-query-d1", Repo: "cloudflare/python-workers-examples", Commit: pythonExamplesCommit, Dir: "query-d1",
			What: "Python Worker on D1", Exercises: []string{"python", "d1"}, Install: InstallNone,
			Setup: [][]string{{"d1", "execute", "quotes", "--file", "db_init.sql"}},
			Probe: func(r *Run) error { return r.Get("/").Want(200, `"quote"`, `"author"`) }},
	}
}
