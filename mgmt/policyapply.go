package mgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/rosskukulinski/tailworks/internal/policy"
	v1 "github.com/rosskukulinski/tailworks/mgmt/api/v1"
	"github.com/tailscale/hujson"
)

// tw policy --apply / tw deploy --apply-policy (issue #116): mgmt adds what
// a bundle's preflight found missing to the tenant's tailnet policy. The
// snippet is built from the preflight's own snippets (what tw policy
// prints is what is written), every entry passes policy.Check (RT-06: only
// tag:tw* tags and sources, never removals), and the write is
// read → merge → validate → If-Match, retried once on a conflict.

// PolicyApplyRequest is POST /v1/tenants/{id}/workloads/{workload}/policy.
type PolicyApplyRequest struct {
	Bundle     []byte        `json:"bundle"`
	Namespaces []ClassConfig `json:"namespaces,omitempty"`
}

// PolicyApplyResult is its answer.
type PolicyApplyResult struct {
	TenantID string `json:"tenantID"`
	Workload string `json:"workload"`
	DryRun   bool   `json:"dryRun"`
	Changed  bool   `json:"changed"`
	// Added is the snippet written (dryRun: to be written), as HuJSON.
	Added string `json:"added,omitempty"`
	// Manual: what is missing that tailworks doesn't write.
	Manual []string `json:"manual,omitempty"`

	// CallerGrant: the grant in Added that lets every user of the tailnet
	// call the app (callergrant.go); "" when Added has none.
	CallerGrant string `json:"callerGrant,omitempty"`

	// CanWrite (dryRun with Added non-empty): the control plane accepted
	// the merged policy, so the write would go through (Tailscale refuses
	// a client without Policy File write). tw deploy asks only then (#481).
	CanWrite *bool `json:"canWrite,omitempty"`

	OldETag string `json:"oldETag,omitempty"`
	NewETag string `json:"newETag,omitempty"`
	// Preflight is the bundle's preflight against the policy as it is
	// after the write (dryRun: as it is now).
	Preflight PreflightResult `json:"preflight"`
}

func (a *v1api) ApplyWorkloadPolicy(w http.ResponseWriter, r *http.Request, id v1.TenantID, workload v1.WorkloadName, params v1.ApplyWorkloadPolicyParams) {
	var req PolicyApplyRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	res, err := a.s.M.ApplyWorkloadPolicy(r.Context(), id, workload, req, boolOf(params.DryRun), principalOf(r.Context()).Actor(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

// ApplyWorkloadPolicy preflights the bundle, then (unless dryRun) adds the
// missing tagOwners, grants, funnel nodeAttrs and the Service's
// autoApprovers.services entry to the tenant's policy.
func (m *Mgmt) ApplyWorkloadPolicy(ctx context.Context, tenantID, workload string, r PolicyApplyRequest, dryRun bool, by string) (PolicyApplyResult, error) {
	res := PolicyApplyResult{TenantID: tenantID, Workload: workload, DryRun: dryRun}
	t, err := m.store.GetTenant(ctx, tenantID)
	if err != nil {
		return res, lookupErr(err, errf(404, "unknown_tenant", "tenant %q not found", tenantID))
	}
	if len(r.Bundle) == 0 && len(r.Namespaces) == 0 {
		return res, errf(400, "bad_request", "bundle is required")
	}
	dr := DeployRequest{TenantID: tenantID, Workload: workload, Bundle: r.Bundle, Namespaces: r.Namespaces, By: by}
	m.policies.Invalidate(t.ID) // plan against the policy as it is now, not the cache
	pf, err := m.PreflightBundle(ctx, dr)
	if err != nil {
		return res, err
	}
	res.Preflight = pf
	popts := policy.Options{Headscale: t.Headscale()}
	defined := func(string) bool { return true }
	pol, perr := m.policies.Get(ctx, t)
	if perr == nil {
		defined = func(tag string) bool { _, ok := pol.TagOwners[tag]; return ok }
	} else {
		pol = nil
	}
	snip, manual, err := preflightSnippet(pf, popts, defined)
	if err != nil {
		return res, err
	}
	// The caller grant (callergrant.go) is mgmt's own, added after the
	// bundle's entries passed Check without its relaxation.
	if cfg, cerr := BundleConfig(r.Bundle); len(r.Bundle) > 0 && cerr == nil {
		cg, why := callerGrantPorts(cfg.Tailnet.Tags, CallerPorts(cfg.Tailnet.HTTPS), snip, pol, popts) // callergrant_https.go
		if why != "" {
			manual = append(manual, why)
		}
		if cg != nil {
			snip.Grants = append(snip.Grants, cg)
			if !onlyCallerGrant(snip, cg) {
				return res, errf(500, "internal", "the policy snippet has a grant from outside tag:tw* that isn't the caller grant")
			}
			popts = callerGrantOptions(popts)
			cb, _ := json.Marshal(cg)
			res.CallerGrant = string(cb)
		}
	}
	res.Manual = manual
	res.Added = snip.HuJSON()
	if snip.Empty() {
		return res, nil
	}
	if dryRun {
		can := m.policyWritable(ctx, t, snip, popts) // policyapply_canwrite.go
		res.CanWrite = &can
		return res, nil
	}
	fail := func(e *Error) (PolicyApplyResult, error) {
		m.audit(ctx, AuditRecord{TenantID: tenantID, Kind: "policy_apply", Code: e.Code, ActingFor: by},
			map[string]any{"workload": workload, "ok": false, "error": e.Message, "by": by})
		return res, e
	}
	api, err := m.broker.API(ctx, t, purposePolicy)
	if err != nil {
		return fail(errf(503, "policy_unavailable", "can't reach the tenant's policy: %v", err))
	}
	pa, ok := api.(policy.API)
	if !ok {
		return fail(errf(403, "policy_write_denied", "tenant %s's credential can't write the tailnet policy", tenantID))
	}
	ar, err := policy.ApplyRetry(ctx, pa, snip, popts)
	switch {
	case errors.Is(err, policy.ErrConflict):
		return fail(errf(409, "policy_conflict", "%v", err))
	case err != nil && policy.IsForbidden(err):
		return fail(errf(403, "policy_write_denied", "the tenant's OAuth client may not write the tailnet policy (it needs the Policy File write scope): %v", err))
	case err != nil:
		return fail(errf(500, "policy_apply_failed", "%v", err))
	}
	res.Changed, res.OldETag, res.NewETag, res.Added = ar.Changed, ar.OldETag, ar.NewETag, ar.Added.HuJSON()
	m.audit(ctx, AuditRecord{TenantID: tenantID, Kind: "policy_apply", ActingFor: by}, map[string]any{"workload": workload,
		"ok": true, "changed": ar.Changed, "added": res.Added, "callerGrant": res.CallerGrant, "oldETag": ar.OldETag, "newETag": ar.NewETag, "by": by})
	m.policies.Invalidate(t.ID)
	if pf, err := m.PreflightBundle(ctx, dr); err == nil {
		res.Preflight = pf
	}
	return res, nil
}

// preflightSnippet turns a preflight's missing parts into the snippet
// tailworks may write, parsed from the preflight's own snippets. Entries
// policy.Check refuses, and what Snippet can't express (a team tenant's
// ownership problems), are returned as manual lines instead, as are grants
// to a tag the policy doesn't define (defined): the control plane rejects
// the whole policy for one of those. A Service's autoApprovers entry is
// written only for the workload's own tags (serviceApprover).
func preflightSnippet(pf PreflightResult, o policy.Options, defined func(tag string) bool) (policy.Snippet, []string, error) {
	var s policy.Snippet
	var manual []string
	add := func(part string) error {
		var p policy.Snippet
		if err := parseHuJSONObject(part, &p); err != nil {
			return errf(500, "internal", "preflight snippet %q: %v", part, err)
		}
		for t, owners := range p.TagOwners {
			if s.TagOwners == nil {
				s.TagOwners = map[string][]string{}
			}
			s.TagOwners[t] = owners
		}
		s.Grants = append(s.Grants, p.Grants...)
		s.NodeAttrs = append(s.NodeAttrs, p.NodeAttrs...)
		return nil
	}
	if pf.TagOwnersSnippet != "" && (len(pf.MissingTagOwners) > 0 || len(pf.UndefinedConnects) > 0) {
		if err := add(pf.TagOwnersSnippet); err != nil {
			return s, nil, err
		}
	}
	for _, g := range pf.MissingGrants() {
		if err := add(`"grants": [` + g.Snippet + `]`); err != nil {
			return s, nil, err
		}
	}
	if len(pf.MissingNodeAttrs) > 0 && pf.NodeAttrsSnippet != "" {
		if err := add(pf.NodeAttrsSnippet); err != nil {
			return s, nil, err
		}
	}
	// Keep only what Check admits, one entry at a time.
	var keep policy.Snippet
	tags := make([]string, 0, len(s.TagOwners))
	for t := range s.TagOwners {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	for _, t := range tags {
		one := policy.Snippet{TagOwners: map[string][]string{t: s.TagOwners[t]}}
		if err := policy.Check(one, o); err != nil {
			manual = append(manual, fmt.Sprintf("tagOwners %s (not written: %v)", t, err))
			continue
		}
		if keep.TagOwners == nil {
			keep.TagOwners = map[string][]string{}
		}
		keep.TagOwners[t] = s.TagOwners[t]
	}
	for _, g := range s.Grants {
		if err := policy.Check(policy.Snippet{Grants: []map[string]any{g}}, o); err != nil {
			manual = append(manual, fmt.Sprintf("grant %s: not written: %s", grantLabel(g), checkReason(err)))
			continue
		}
		if u := undefinedDst(g, keep, defined); u != "" {
			manual = append(manual, fmt.Sprintf("grant %s: not written: %s has no tagOwners entry and isn't a tailworks tag (the control plane rejects a grant to an undefined tag); define it in the admin console, or drop it from tailnet.connects", grantLabel(g), u))
			continue
		}
		keep.Grants = append(keep.Grants, g)
	}
	for _, n := range s.NodeAttrs {
		if err := policy.Check(policy.Snippet{NodeAttrs: []map[string]any{n}}, o); err != nil {
			b, _ := json.Marshal(n)
			manual = append(manual, fmt.Sprintf("nodeAttrs %s (not written: %v)", b, err))
			continue
		}
		keep.NodeAttrs = append(keep.NodeAttrs, n)
	}
	if pf.ServiceUnapproved && pf.ServiceSnippet != "" {
		a, why, err := serviceApprover(pf, keep, o, defined)
		if err != nil {
			return s, nil, err
		}
		if why != "" {
			manual = append(manual, "autoApprovers.services "+pf.Service+": not written: "+why+"; add it yourself: "+pf.ServiceSnippet)
		} else {
			keep.AutoApprovers = a
		}
	}
	manual = append(manual, pf.TagOwnerProblems...)
	return keep, manual, nil
}

// serviceApprover is the autoApprovers.services entry for the workload's
// Service (issue #295), parsed from the preflight's snippet. It is written
// only as narrow as it gets: one key, the workload's own svc: name, and
// approvers among the tags the workload node is deployed with, each
// defined in tagOwners (the control plane rejects an undefined tag) and
// passing policy.Check. Otherwise why says what stops it.
func serviceApprover(pf PreflightResult, keep policy.Snippet, o policy.Options, defined func(string) bool) (a policy.AutoApprovers, why string, err error) {
	var p policy.Snippet
	if err := parseHuJSONObject(pf.ServiceSnippet, &p); err != nil {
		return a, "", errf(500, "internal", "preflight snippet %q: %v", pf.ServiceSnippet, err)
	}
	a = p.AutoApprovers
	who, ok := a.Services[pf.Service]
	if len(a.Services) != 1 || !ok {
		return a, fmt.Sprintf("the entry isn't for %s alone", pf.Service), nil
	}
	for _, tag := range who {
		if !slices.Contains(pf.workloadTags, tag) {
			return a, fmt.Sprintf("%s isn't one of the workload's tags", tag), nil
		}
		if _, ok := keep.TagOwners[tag]; !ok && !defined(tag) {
			return a, fmt.Sprintf("%s has no tagOwners entry", tag), nil
		}
	}
	if err := policy.Check(policy.Snippet{AutoApprovers: a}, o); err != nil {
		return a, strings.TrimPrefix(err.Error(), "policy snippet: "), nil
	}
	return a, "", nil
}

// undefinedDst is the first dst tag of g that neither the policy nor the
// snippet's tagOwners defines ("" if none).
func undefinedDst(g map[string]any, s policy.Snippet, defined func(string) bool) string {
	dst, _ := g["dst"].([]any)
	for _, d := range dst {
		tag, _ := d.(string)
		if !strings.HasPrefix(tag, "tag:") {
			continue
		}
		if _, ok := s.TagOwners[tag]; ok || defined(tag) {
			continue
		}
		return tag
	}
	return ""
}

// parseHuJSONObject parses a policy fragment (`"key": value, ...`) as the
// object it is part of.
func parseHuJSONObject(part string, v any) error {
	std, err := hujson.Standardize([]byte("{" + part + "}"))
	if err != nil {
		return err
	}
	return json.Unmarshal(std, v)
}

// checkReason is policy.Check's refusal without its "policy snippet: grant
// N: " prefix (the manual line names the grant already).
func checkReason(err error) string {
	r := strings.TrimPrefix(err.Error(), "policy snippet: ")
	if rest, ok := strings.CutPrefix(r, "grant "); ok {
		if i := strings.Index(rest, ": "); i >= 0 {
			r = rest[i+2:]
		}
	}
	return r
}
