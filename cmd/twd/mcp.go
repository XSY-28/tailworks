package main

// twd mcp (issue #25, ADR-mgmt-mcp): twd's MCP server for coding agents.
//
//	twd mcp [--allow kill,undeploy,deploy]   stdio MCP server: a bridge to mgmt's /mcp
//	twd mcp --config                         how to register it with an agent
//
// The bridge reads newline-delimited JSON-RPC from stdin and POSTs each
// message to mgmt's /mcp over the transport every other command uses (the
// loopback on the twd host, the user's own Tailscale from a `twd link`ed
// machine), so the agent needs no credentials. Who may call which tool is
// decided by mgmt (mgmt/mcp.go): on a linked machine, the user's
// twd.dev/cap/mcp grant; on the twd host, the operator's --allow.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rosskukulinski/twd/hostd/mgmtapi"
	"github.com/rosskukulinski/twd/mgmt"
)

// mcpReadTools are the tools every MCP caller gets (mgmt/mcp.go).
var mcpReadTools = []string{"whoami", "status", "ls", "logs", "audit"}

func cmdMCP(args []string) error {
	f := newFlags("mcp")
	allow := f.String("allow", "", "also allow these tools: deploy, kill, undeploy (comma-separated). On the twd host this is what enables them; from a linked machine your twd.dev/cap/mcp grant decides, and this only narrows")
	config := f.Bool("config", false, "print how to register twd's MCP server with a coding agent, and exit")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fail("bad_request", "twd mcp takes no arguments", "usage: twd "+usageOf("mcp"))
	}
	var tools []string
	if *allow != "" {
		known := map[string]bool{"deploy": true, "kill": true, "undeploy": true, "*": true}
		tools = append(tools, mcpReadTools...)
		for _, t := range strings.Split(*allow, ",") {
			t = strings.TrimSpace(t)
			if !known[t] {
				return fail("bad_request", "--allow: unknown tool "+t, "the tools that need --allow: deploy, kill, undeploy (or *)")
			}
			tools = append(tools, t)
		}
	}
	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	// A linked machine's user may hold only twd.dev/cap/mcp, which admits
	// /mcp and not /v1/whoami: don't probe /v1 there.
	if !s.remote() {
		if err := s.requireUp(); err != nil {
			return err
		}
	}
	if *config {
		return mcpConfig(f, s, *allow)
	}
	hdr := map[string]string{"Twd-Actor": actor()}
	if len(tools) > 0 {
		hdr[mgmt.MCPToolsHeader] = strings.Join(tools, ",")
	}
	return mcpBridge(s.mgmt(), hdr, os.Stdin, os.Stdout)
}

// mcpBridge relays newline-delimited JSON-RPC between in/out and mgmt's
// /mcp. A refusal from mgmt (no grant at all, mgmt down) becomes a JSON-RPC
// error carrying the twd error, so the agent sees the code and the fix.
func mcpBridge(c *mgmtapi.Client, hdr map[string]string, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			enc.Encode(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "parse error: " + err.Error()}})
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		var resp []byte
		err := c.DoHeaders(ctx, "POST", mgmt.MCPPath, json.RawMessage(line), &resp, hdr)
		cancel()
		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			continue // a notification: nothing to answer
		}
		if err != nil {
			e := jsonError(asCLIErr(err))
			enc.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32000, "message": e.Code + ": " + e.Message, "data": e}})
			continue
		}
		if _, err := out.Write(append([]byte(strings.TrimSpace(string(resp))), '\n')); err != nil {
			return nil // the agent went away
		}
	}
	return nil
}

type mcpConfigResult struct {
	URL       string            `json:"url"`
	Stdio     []string          `json:"stdio"`
	Tools     []string          `json:"tools"`
	Commands  map[string]string `json:"commands"`
	Grant     string            `json:"grant,omitempty"`
	GrantNote string            `json:"grantNote,omitempty"`
}

// mcpConfig prints how to register the server: the stdio bridge (works
// wherever twd works) and the URL (for agents on a tailnet machine).
func mcpConfig(f *flags, s *state, allow string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res := mcpConfigResult{Stdio: []string{"twd", "mcp"}, Commands: map[string]string{}}
	if s.remote() {
		res.URL = strings.TrimSuffix(s.Link.Endpoint, "/") + mgmt.MCPPath
	} else {
		w, err := s.whoami(ctx)
		if err != nil {
			return err
		}
		scheme := "http"
		if w.Mgmt.HTTPS {
			scheme = "https"
		}
		res.URL = scheme + "://" + w.Mgmt.DNSName + mgmt.MCPPath
	}
	if allow != "" {
		res.Stdio = append(res.Stdio, "--allow", allow)
	}
	// Who mgmt says we are, and what we may call: the whoami tool.
	var who struct {
		Result struct {
			StructuredContent struct {
				Kind       string   `json:"kind"`
				Tools      []string `json:"tools"`
				HowToGrant string   `json:"howToGrant"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	hdr := map[string]string{}
	if allow != "" {
		hdr[mgmt.MCPToolsHeader] = strings.Join(append(append([]string{}, mcpReadTools...), strings.Split(allow, ",")...), ",")
	}
	call := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "whoami", "arguments": map[string]any{}}}
	if err := s.mgmt().DoHeaders(ctx, "POST", mgmt.MCPPath, call, &who, hdr); err != nil {
		return linkErr(s.Link, err)
	}
	wc := who.Result.StructuredContent
	res.Tools = append([]string{}, wc.Tools...)
	res.Commands["claude"] = "claude mcp add twd -- " + strings.Join(res.Stdio, " ")
	res.Commands["json"] = `{"mcpServers": {"twd": {"command": "twd", "args": ` + jsonList(res.Stdio[1:]) + `}}}`
	if wc.Kind != "local" && wc.HowToGrant != "" {
		res.Grant = wc.HowToGrant
		res.GrantNote = "the tools you don't have need a twd.dev/cap/mcp grant naming them; add only those the agent should have"
	}
	emit(f, res, func() {
		fmt.Printf("twd MCP server · tools you may call: %s\n\n", strings.Join(res.Tools, ", "))
		fmt.Printf("stdio (any agent; runs through this twd CLI, no credentials):\n  %s\n  %s\n\n", res.Commands["claude"], res.Commands["json"])
		fmt.Printf("HTTP (an agent on a machine on your tailnet):\n  %s\n", res.URL)
		if res.Grant != "" {
			fmt.Printf("\n%s:\n  %s\n", res.GrantNote, res.Grant)
		} else if allow == "" {
			fmt.Printf("\non this host, kill, undeploy and deploy are off unless you add them: twd mcp --allow kill,undeploy\n")
		}
	})
	return nil
}

func jsonList(xs []string) string {
	b, _ := json.Marshal(xs)
	return string(b)
}
