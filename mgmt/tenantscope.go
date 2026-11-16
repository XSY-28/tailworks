package mgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Tenant admins (ADR-team-tenants D2, D3): a team manages its own tenants
// through a grant of TenantAdminCap, and nothing else.
//
// TenantAdminCap is deliberately not a value on AdminCap: mgmt treats
// *holding* AdminCap as full admin and ignores its values, so a scoped
// value there would be full admin on any mgmt that predates this file. A
// capability older versions don't know is refused by them (fail closed).
//
// A tenant admin is an ordinary "user" principal without AdminCap. Every
// existing check that asks "is this user an admin?" therefore says no, and
// the only extra access is what this file grants: an operation whose
// x-twd-role has a "tenant:<action>" part, for a tenant one single grant
// value names together with that action.

// TenantAdminCap is the app capability (granted to tag:twd-mgmt) that lets
// a tailnet caller act in named tenants. Values:
//
//	{"tenants": ["payments"], "actions": ["read", "deploy"]}
//
// Both fields are required and non-empty; a value missing either, or naming
// an unknown action, grants nothing.
const TenantAdminCap = "twd.dev/cap/tenant-admin"

// Tenant actions, the "tenant:<action>" part of an operation's x-twd-role.
const (
	ActionData    = "data"    // tenant data through mgmt: KV, D1, store journals, queues
	ActionDeploy  = "deploy"  // deploy, rollback, promote, undeploy, public routes, telemetry, notifications
	ActionKill    = "kill"    // kill objects, set their TTL
	ActionRead    = "read"    // list and read: deployments, objects, logs, audit, config
	ActionSecrets = "secrets" // write and delete secret values
)

// TenantActions are the known actions, sorted.
var TenantActions = []string{
	ActionData,
	ActionDeploy,
	ActionKill,
	ActionRead,
	ActionSecrets,
}

// TenantGrant is one valid TenantAdminCap value.
type TenantGrant struct {
	Tenants []string `json:"tenants"`
	Actions []string `json:"actions"`
}

// allows: this one value names both the tenant and the action.
func (g TenantGrant) allows(tenant, action string) bool {
	return slices.Contains(g.Tenants, tenant) && slices.Contains(g.Actions, action)
}

// parseTenantGrants splits the cap's values into valid grants and the
// reasons the others grant nothing.
func parseTenantGrants(vals []json.RawMessage) (grants []TenantGrant, invalid []string) {
	for _, raw := range vals {
		var g TenantGrant
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&g); err != nil {
			invalid = append(invalid, fmt.Sprintf("%s: not {\"tenants\": [...], \"actions\": [...]} (%v)", raw, err))
			continue
		}
		switch {
		case len(g.Tenants) == 0:
			invalid = append(invalid, fmt.Sprintf("%s: names no tenants", raw))
			continue
		case len(g.Actions) == 0:
			invalid = append(invalid, fmt.Sprintf("%s: names no actions", raw))
			continue
		case slices.Contains(g.Tenants, "") || slices.Contains(g.Tenants, "*"):
			invalid = append(invalid, fmt.Sprintf("%s: tenants must be tenant IDs (there is no wildcard; operators hold %s)", raw, AdminCap))
			continue
		}
		bad := ""
		for _, a := range g.Actions {
			if !slices.Contains(TenantActions, a) {
				bad = a
				break
			}
		}
		if bad != "" {
			invalid = append(invalid, fmt.Sprintf("%s: unknown action %q (want %s)", raw, bad, strings.Join(TenantActions, ", ")))
			continue
		}
		grants = append(grants, g)
	}
	return grants, invalid
}

// tenantGrants are p's valid TenantAdminCap values (nil for operators,
// hosts and the local principal, which this file never narrows).
func (p Principal) tenantGrants() []TenantGrant {
	if p.Kind != "user" {
		return nil
	}
	g, _ := parseTenantGrants(p.CapValues[TenantAdminCap])
	return g
}

// tenantScoped: p is a tenant admin, i.e. a user that is not an operator
// and holds at least one valid TenantAdminCap value.
func (s *Server) tenantScoped(p Principal) bool {
	return p.Kind == "user" && !s.allowed(p, roleAdmin) && len(p.tenantGrants()) > 0
}

// holdsTenantAdminCap: a non-operator user holding TenantAdminCap, valid
// values or not. TailnetHandler admits it so GET /v1/whoami can say what's
// wrong with its values; every other route still needs a valid one.
func (s *Server) holdsTenantAdminCap(p Principal) bool {
	return p.Kind == "user" && !s.allowed(p, roleAdmin) && (slices.Contains(p.Caps, TenantAdminCap) || len(p.CapValues[TenantAdminCap]) > 0)
}

// noValidTenantGrant is the refusal for a tenant-admin holder whose values
// all grant nothing.
func noValidTenantGrant(p Principal) *Error {
	_, bad := parseTenantGrants(p.CapValues[TenantAdminCap])
	e := errf(403, "tenant_forbidden", "%s holds %s, but no value grants anything: %s", p.Login, TenantAdminCap, strings.Join(bad, "; "))
	e.Missing = map[string]any{"cap": TenantAdminCap}
	e.Snippet = TenantAdminGrantSnippet(p.Login, "<tenant>", ActionRead)
	return e
}

// tenantAllows: one single value of p's grants names tenant and action.
func (p Principal) tenantAllows(tenant, action string) bool {
	for _, g := range p.tenantGrants() {
		if g.allows(tenant, action) {
			return true
		}
	}
	return false
}

// tenantsWith lists the tenants p may act in with action, sorted.
func (p Principal) tenantsWith(action string) []string {
	var out []string
	for _, g := range p.tenantGrants() {
		if slices.Contains(g.Actions, action) {
			for _, t := range g.Tenants {
				if !slices.Contains(out, t) {
					out = append(out, t)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// tenantAction is the "tenant:<action>" part of an x-twd-role ("" if none).
func tenantAction(role string) string {
	for _, part := range strings.Split(role, "|") {
		if a, ok := strings.CutPrefix(strings.TrimSpace(part), "tenant:"); ok {
			return a
		}
	}
	return ""
}

// tenantPathPrefix: operations whose path carries the tenant; the gate
// checks the tenant itself.
const tenantPathPrefix = "/v1/tenants/{id}"

// tenantCheckedInHandler are the tenant-admin operations whose tenant is
// not in the path: the gate admits a tenant admin who holds the action in
// any tenant, and the handler checks the request's tenant (tenantOK,
// tenantListScope). An operation with a "tenant:" part that is neither
// under tenantPathPrefix nor listed here (or in tenantAnyTenant) is refused
// to tenant admins, so the spec alone can't open a route
// (TestTenantRolesConsistent).
var tenantCheckedInHandler = map[string]bool{
	"DELETE /v1/deployments/{id}":     true,
	"DELETE /v1/objects/{id}":         true,
	"GET /v1/audit":                   true,
	"GET /v1/deployments":             true,
	"GET /v1/deployments/{id}":        true,
	"GET /v1/deployments/{id}/bundle": true,
	"GET /v1/objects":                 true,
	"GET /v1/objects/{id}":            true,
	"GET /v1/tenants":                 true,
	"PATCH /v1/objects/{id}":          true,
	"POST /v1/deployments":            true,
}

// tenantAnyTenant are tenant-admin operations that read nothing
// tenant-specific (operator base images and their aliases): holding the
// action in any tenant is enough.
var tenantAnyTenant = map[string]bool{
	"GET /v1/images/aliases": true,
	"GET /v1/images/public":  true,
}

type tenantActionKey struct{}

// tenantGate decides a tenant admin's request after roleAllowed refused it.
// It reports whether the request may go on (with the action in the
// context for the handler) and writes the refusal otherwise.
func (s *Server) tenantGate(w http.ResponseWriter, r *http.Request, p Principal, role, pattern string) (*http.Request, bool) {
	action := tenantAction(role)
	method, path, _ := strings.Cut(pattern, " ")
	if action == "" {
		e := errf(403, "tenant_forbidden", "%s %s is an operator route: a %s grant can't reach it (operators hold %s)", method, path, TenantAdminCap, AdminCap)
		e.Missing = map[string]any{"cap": AdminCap}
		writeErr(w, r, e)
		return r, false
	}
	switch {
	case path == tenantPathPrefix || strings.HasPrefix(path, tenantPathPrefix+"/"):
		// mux.Handler resolved the pattern without binding it, so read the
		// segment the mux will bind: split the escaped path (an encoded
		// "/" stays inside its segment), then unescape.
		tenant := ""
		if parts := strings.SplitN(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/", 4); len(parts) >= 3 {
			tenant, _ = url.PathUnescape(parts[2])
		}
		if !p.tenantAllows(tenant, action) {
			writeErr(w, r, tenantForbidden(p, tenant, action))
			return r, false
		}
	case tenantCheckedInHandler[pattern], tenantAnyTenant[pattern]:
		if len(p.tenantsWith(action)) == 0 {
			writeErr(w, r, tenantForbidden(p, "", action))
			return r, false
		}
	default:
		writeErr(w, r, errf(403, "tenant_forbidden", "%s %s is not open to tenant admins", method, path))
		return r, false
	}
	return r.WithContext(context.WithValue(r.Context(), tenantActionKey{}, action)), true
}

// tenantForbidden is the typed refusal, with the grant value to add.
func tenantForbidden(p Principal, tenant, action string) *Error {
	who := p.Login
	src := p.Login
	if len(p.Tags) > 0 {
		src = p.Tags[0]
	}
	target := tenant
	if target == "" {
		target = "<tenant>"
	}
	var e *Error
	if tenant == "" {
		e = errf(403, "tenant_forbidden", "%s holds %q in no tenant: no %s value names it", who, action, TenantAdminCap)
	} else {
		e = errf(403, "tenant_forbidden", "%s may not %q in tenant %s: no single %s value names both", who, action, tenant, TenantAdminCap)
	}
	e.Missing = map[string]any{"src": src, "dst": MgmtTag, "cap": TenantAdminCap, "tenant": tenant, "action": action}
	e.Snippet = TenantAdminGrantSnippet(src, target, action)
	return e
}

// TenantAdminGrantSnippet is the policy grant that lets src take action in
// tenant through mgmt's API.
func TenantAdminGrantSnippet(src, tenant, action string) string {
	return `{"src": ["` + src + `"], "dst": ["` + MgmtTag + `"], "ip": ["tcp:443", "tcp:80"], "app": {"` + TenantAdminCap +
		`": [{"tenants": ["` + tenant + `"], "actions": ["` + action + `"]}]}}`
}

// tenantOK is the handler-side check for tenantCheckedInHandler operations:
// nil unless the caller is a tenant admin whose grants don't name tenant
// with the gate's action. named says the caller supplied the tenant ID
// (body or query): the refusal is a 403 before any lookup. Otherwise the
// tenant came from a resource, and the refusal is a 404, so another
// tenant's resources stay invisible.
func (a *v1api) tenantOK(r *http.Request, tenant string, named bool) error {
	p := principalOf(r.Context())
	action, _ := r.Context().Value(tenantActionKey{}).(string)
	if action == "" || !a.s.tenantScoped(p) {
		return nil
	}
	if p.tenantAllows(tenant, action) {
		return nil
	}
	if named {
		return tenantForbidden(p, tenant, action)
	}
	return errf(404, "not_found", "not found")
}

// tenantListScope is the handler-side scope of a list operation. For a
// tenant admin: with a tenant filter, the filter is checked; without one,
// it defaults to the caller's only tenant for the action, and a caller
// with several must name one. scoped is false for everyone else (they see
// what they saw before).
func (a *v1api) tenantListScope(r *http.Request, filter *string) (tenant string, scoped bool, err error) {
	p := principalOf(r.Context())
	action, _ := r.Context().Value(tenantActionKey{}).(string)
	if action == "" || !a.s.tenantScoped(p) {
		return str(filter), false, nil
	}
	if f := str(filter); f != "" {
		if !p.tenantAllows(f, action) {
			return "", true, tenantForbidden(p, f, action)
		}
		return f, true, nil
	}
	mine := p.tenantsWith(action)
	if len(mine) == 1 {
		return mine[0], true, nil
	}
	e := errf(400, "bad_request", "name a tenant with ?tenant= (your %s grants cover %s)", TenantAdminCap, strings.Join(mine, ", "))
	e.Missing = map[string]any{"param": "tenant"}
	return "", true, e
}

// whoamiTenantGrants is whoami's view of p's TenantAdminCap values: the
// valid ones, and why the others grant nothing. An operator's values are
// reported too (they don't narrow it; AdminCap wins).
func whoamiTenantGrants(p Principal) ([]TenantGrant, []string) {
	g, bad := parseTenantGrants(p.CapValues[TenantAdminCap])
	if g == nil {
		g = []TenantGrant{}
	}
	return g, bad
}
