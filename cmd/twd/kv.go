package main

// Workers KV (ADR-036), CLI side, like `wrangler kv`:
//
//	twd kv namespace create|list|delete [NAME]
//	twd kv key put <KEY> [VALUE] | get <KEY> | delete <KEY> | list [--prefix P]
//
// A namespace belongs to a workload (default: the app in the current
// directory). Key commands name it with --namespace NAME, or with --binding
// BINDING, looked up in the app's wrangler config kv_namespaces as a deploy
// would. Everything goes to mgmt's /v1 API: the namespace is a platform
// store of kind "kv", and key writes are journal appends a running
// workload sees on its next sync (within 60 s).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rosskukulinski/twd/internal/kvwire"
	"github.com/rosskukulinski/twd/mgmt"
)

// kvInput is where `twd kv key put` reads a value given neither as an
// argument nor with --path (tests replace it).
var kvInput = func() io.Reader { return os.Stdin }

// kvOutput is where `twd kv key get` writes the raw value (tests replace it).
var kvOutput = func() io.Writer { return os.Stdout }

type kvResult struct {
	Workload   string            `json:"workload"`
	TenantID   string            `json:"tenantID"`
	Namespace  string            `json:"namespace,omitempty"`
	Namespaces *[]mgmt.StoreInfo `json:"namespaces,omitempty"` // namespace list: always present
	Created    *bool             `json:"created,omitempty"`
	Deleted    string            `json:"deleted,omitempty"`
	Key        *mgmt.KVKey       `json:"key,omitempty"`
	Keys       *[]mgmt.KVKey     `json:"keys,omitempty"` // key list: always present
	Value      *mgmt.KVValue     `json:"value,omitempty"`
}

func kvUsage() string { return "usage: twd " + usageOf("kv") }

func cmdKV(args []string) error {
	f := newFlags("kv")
	tenantF := tenantFlag(f)
	workloadF := f.String("workload", "", "workload whose namespaces these are (default: the app in the current directory)")
	nsF := f.String("namespace", "", "key commands: the namespace's name (its kv_namespaces id)")
	bindingF := f.String("binding", "", "key commands: the namespace bound as this binding in the app's wrangler.jsonc")
	prefixF := f.String("prefix", "", "key list: only keys starting with this")
	pathF := f.String("path", "", "key put: read the value from this file")
	ttlF := f.Int64("ttl", 0, "key put: expire after this many seconds (≥ 60)")
	expF := f.Int64("expiration", 0, "key put: expire at this Unix time (≥ 60 s ahead)")
	metaF := f.String("metadata", "", "key put: metadata (JSON, ≤ 1024 bytes)")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return fail("bad_request", "twd kv needs a command: namespace create|list|delete, key put|get|delete|list", kvUsage())
	}
	group, verb, rest := pos[0], pos[1], pos[2:]
	switch verb {
	case "ls":
		verb = "list"
	case "rm":
		verb = "delete"
	}
	need := func(n int, what string) error {
		if len(rest) != n {
			return fail("bad_request", "twd kv "+group+" "+verb+" "+what, kvUsage())
		}
		return nil
	}
	switch group + " " + verb {
	case "namespace create", "namespace delete":
		err = need(1, "needs the namespace's NAME")
	case "namespace list", "key list":
		err = need(0, "takes no arguments")
	case "key get", "key delete":
		err = need(1, "needs the KEY")
	case "key put":
		if len(rest) < 1 || len(rest) > 2 || (len(rest) == 2 && *pathF != "") {
			err = fail("bad_request", "twd kv key put needs the KEY and the value: an argument, --path FILE, or stdin", kvUsage())
		}
	default:
		err = fail("bad_request", fmt.Sprintf("unknown command %q", group+" "+verb), kvUsage())
	}
	if err != nil {
		return err
	}

	// The workload, and for key commands the namespace.
	workload := *workloadF
	var a *app
	if workload == "" || *bindingF != "" {
		if a, err = loadApp(".", ""); err != nil {
			return fail("bad_request", "no app in the current directory to take the workload (or --binding) from").
				withFix("cd <app dir> && twd kv …", "twd kv … --workload <workload> --namespace <name>")
		}
		if workload == "" {
			workload = a.Name
		}
	}
	if workloadLabel(workload) != workload {
		return fail("bad_request", fmt.Sprintf("workload %q is not a DNS label", workload))
	}
	ns := ""
	switch {
	case group == "namespace" && len(rest) == 1:
		ns = rest[0]
	case group == "key":
		if ns, err = kvNamespaceOf(a, *nsF, *bindingF); err != nil {
			return err
		}
	}
	if ns != "" && !mgmt.ValidStoreName(ns) {
		return fail("bad_request", fmt.Sprintf("namespace name %q must be 1-64 of [A-Za-z0-9_.-], starting with a letter or digit", ns))
	}
	key := ""
	if group == "key" && len(rest) > 0 {
		key = rest[0]
		if err := kvwire.CheckKey(key); err != nil {
			return fail("bad_request", "key: "+err.Error())
		}
	}
	var put mgmt.KVPut
	if group+" "+verb == "key put" {
		if put, err = kvPutBody(rest, *pathF, *ttlF, *expF, *metaF); err != nil {
			return err
		}
	}

	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	if err := s.requireUp(); err != nil {
		return err
	}
	tn, err := s.tenant(*tenantF)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stores := "/v1/tenants/" + url.PathEscape(tn.TenantID) + "/workloads/" + url.PathEscape(workload) + "/stores"
	nsPath := stores + "/kv/" + url.PathEscape(ns)
	valPath := nsPath + "/value?key=" + url.QueryEscape(key)
	res := kvResult{Workload: workload, TenantID: tn.TenantID, Namespace: ns}
	mc := s.mgmt()
	hdr := map[string]string{"Twd-Actor": actor()}

	switch group + " " + verb {
	case "namespace create":
		var st mgmt.StoreInfo
		created := mc.Do(ctx, "GET", nsPath, nil, &st) != nil
		if created {
			if err := mc.DoHeaders(ctx, "PUT", nsPath, nil, &st, hdr); err != nil {
				return err
			}
		}
		res.Created = &created
		emit(f, res, func() {
			verb := "created"
			if !created {
				verb = "already exists"
			}
			fmt.Printf("✓ KV namespace %s %s for workload %s (tenant %s)\n", ns, verb, workload, tn.Tailnet)
			fmt.Printf("  bind it in wrangler.jsonc: \"kv_namespaces\": [{ \"binding\": \"MY_KV\", \"id\": %q }]\n", ns)
		})
	case "namespace list":
		items, err := listAll[mgmt.StoreInfo](ctx, mc, stores+"?kind=kv", 0)
		if err != nil {
			return err
		}
		nss := append([]mgmt.StoreInfo{}, items...)
		res.Namespaces = &nss
		emit(f, res, func() {
			if len(items) == 0 {
				fmt.Printf("no KV namespaces for workload %s (tenant %s) · create one: twd kv namespace create <NAME>, or deploy an app with kv_namespaces\n", workload, tn.Tailnet)
				return
			}
			fmt.Printf("%-32s %10s %12s  %s\n", "NAME", "ENTRIES", "BYTES", "UPDATED")
			for _, st := range items {
				fmt.Printf("%-32s %10d %12d  %s\n", st.Name, st.Entries, st.Bytes, time.UnixMilli(st.UpdatedAt).Local().Format("2006-01-02 15:04:05"))
			}
		})
	case "namespace delete":
		if err := mc.DoHeaders(ctx, "DELETE", nsPath, nil, nil, hdr); err != nil {
			return err
		}
		res.Deleted = ns
		emit(f, res, func() {
			fmt.Printf("✓ KV namespace %s of workload %s deleted, with its data (tenant %s)\n", ns, workload, tn.Tailnet)
			fmt.Printf("  a running workload empties its copy on its next sync; its next write starts a new, empty namespace\n")
		})
	case "key put":
		var k mgmt.KVKey
		if err := mc.DoHeaders(ctx, "PUT", valPath, put, &k, hdr); err != nil {
			return err
		}
		res.Key = &k
		emit(f, res, func() {
			fmt.Printf("✓ wrote %q to KV namespace %s of workload %s (%d bytes)\n", key, ns, workload, len(put.Value))
			fmt.Printf("  running workloads see it on their next sync (within 60 s)\n")
		})
	case "key get":
		var v mgmt.KVValue
		if err := mc.Do(ctx, "GET", valPath, nil, &v); err != nil {
			return err
		}
		res.Value = &v
		if *f.json {
			emit(f, res, nil)
			return nil
		}
		_, err := kvOutput().Write(v.Value)
		return err
	case "key delete":
		if err := mc.DoHeaders(ctx, "DELETE", valPath, nil, nil, hdr); err != nil {
			return err
		}
		res.Deleted = key
		emit(f, res, func() { fmt.Printf("✓ deleted %q from KV namespace %s of workload %s\n", key, ns, workload) })
	case "key list":
		keys := []mgmt.KVKey{}
		res.Keys = &keys
		cursor := ""
		for {
			var p mgmt.KVKeyPage
			q := url.Values{"prefix": {*prefixF}, "cursor": {cursor}}
			if err := mc.Do(ctx, "GET", nsPath+"/keys?"+q.Encode(), nil, &p); err != nil {
				return err
			}
			keys = append(keys, p.Keys...)
			if p.ListComplete {
				break
			}
			cursor = p.Cursor
		}
		emit(f, res, func() {
			if len(keys) == 0 {
				fmt.Printf("no keys in KV namespace %s of workload %s\n", ns, workload)
				return
			}
			fmt.Printf("%-40s %-20s %s\n", "KEY", "EXPIRES", "METADATA")
			for _, k := range keys {
				exp := "-"
				if k.Expiration != 0 {
					exp = time.Unix(k.Expiration, 0).Local().Format("2006-01-02 15:04:05")
				}
				meta := "-"
				if len(k.Metadata) > 0 {
					meta = string(k.Metadata)
				}
				fmt.Printf("%-40s %-20s %s\n", strconv.Quote(k.Name), exp, meta)
			}
		})
	}
	return nil
}

// kvNamespaceOf resolves a key command's namespace: --namespace, or
// --binding looked up in the app's kv_namespaces (id, else the binding).
func kvNamespaceOf(a *app, ns, binding string) (string, error) {
	switch {
	case ns != "" && binding != "":
		return "", fail("bad_request", "give --namespace or --binding, not both", kvUsage())
	case ns != "":
		return ns, nil
	case binding == "":
		return "", fail("bad_request", "key commands need the namespace: --binding <BINDING> (from wrangler.jsonc) or --namespace <NAME>", kvUsage())
	}
	list, _ := a.Wrangler["kv_namespaces"].([]any)
	var bound []string
	for _, e := range list {
		m, _ := e.(map[string]any)
		b, _ := m["binding"].(string)
		bound = append(bound, b)
		if b != binding {
			continue
		}
		if id, _ := m["id"].(string); id != "" {
			return id, nil
		}
		return b, nil
	}
	return "", fail("bad_request", fmt.Sprintf("%s has no kv_namespaces binding %q (it has: %s)", orAny(a.WranglerFile, "the wrangler config"), binding, strings.Join(bound, ", ")))
}

func kvPutBody(rest []string, path string, ttl, exp int64, meta string) (mgmt.KVPut, error) {
	var p mgmt.KVPut
	var err error
	switch {
	case len(rest) == 2:
		p.Value = []byte(rest[1])
	case path != "":
		if p.Value, err = os.ReadFile(path); err != nil {
			return p, fail("bad_request", "--path: "+err.Error())
		}
	default:
		if p.Value, err = io.ReadAll(io.LimitReader(kvInput(), kvwire.MaxValueBytes+1)); err != nil {
			return p, fail("bad_request", "reading the value from stdin: "+err.Error())
		}
	}
	if len(p.Value) > kvwire.MaxValueBytes {
		return p, fail("bad_request", fmt.Sprintf("the value is over %d bytes (KV's limit on twd)", kvwire.MaxValueBytes))
	}
	p.ExpirationTTL, p.Expiration = ttl, exp
	if meta != "" {
		if !json.Valid([]byte(meta)) {
			return p, fail("bad_request", "--metadata must be JSON")
		}
		var b bytes.Buffer
		json.Compact(&b, []byte(meta))
		p.Metadata = b.Bytes()
	}
	return p, nil
}
