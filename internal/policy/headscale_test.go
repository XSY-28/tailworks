package policy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func TestHeadscaleProfileSnippet(t *testing.T) {
	s := InfraSnippet(7474)
	s.Grants = append(s.Grants, AdminGrant("autogroup:admin", "tag:twd-mgmt", "example.com/cap/admin"))
	s.NodeAttrs = PublicSnippet().NodeAttrs
	hs := Headscale(s)
	if err := Check(hs, Options{Headscale: true}); err != nil {
		t.Fatalf("Check(Headscale(infra)): %v", err)
	}
	if got := hs.TagOwners["tag:twd"]; len(got) != 0 {
		t.Errorf("tag:twd owners = %v, want none", got)
	}
	if got := hs.TagOwners["tag:twd-mgmt"]; len(got) != 1 || got[0] != "tag:twd" {
		t.Errorf("tag:twd-mgmt owners = %v, want [tag:twd]", got)
	}
	if len(hs.NodeAttrs) != 0 || len(hs.Grants) != 2 {
		t.Errorf("nodeAttrs %v grants %v: want none and the 2 infra grants", hs.NodeAttrs, hs.Grants)
	}
	// The input is untouched.
	if len(s.TagOwners["tag:twd"]) != 1 {
		t.Errorf("input modified: %v", s.TagOwners)
	}
	// Merged into an empty policy it is valid HuJSON with every tag defined.
	out, err := MergeWith([]byte("{}"), hs, Options{Headscale: true})
	if err != nil {
		t.Fatal(err)
	}
	std, err := hujson.Standardize(out)
	if err != nil {
		t.Fatalf("merged policy: %v\n%s", err, out)
	}
	var d struct {
		TagOwners map[string][]string `json:"tagOwners"`
	}
	if err := json.Unmarshal(std, &d); err != nil {
		t.Fatal(err)
	}
	if o, ok := d.TagOwners["tag:twd"]; !ok || len(o) != 0 {
		t.Errorf("merged tag:twd owners = %v (present %v), want []", o, ok)
	}
	miss, err := Missing(out, hs)
	if err != nil || !miss.Empty() {
		t.Errorf("Missing after merge = %+v, %v", miss, err)
	}
}

func TestHeadscaleProfileCheckRejects(t *testing.T) {
	cases := map[string]Snippet{
		"autogroup owner": {TagOwners: map[string][]string{"tag:twd-x": {"autogroup:admin"}}},
		"svc dst":         {Grants: []map[string]any{{"src": []string{"tag:twd"}, "dst": []string{"svc:x"}, "ip": []string{"tcp:443"}}}},
		"funnel":          PublicSnippet(),
		"autogroup src":   {Grants: []map[string]any{{"src": []string{"autogroup:admin"}, "dst": []string{"tag:twd-mgmt"}, "ip": []string{"tcp:443"}}}},
		"posture":         {Grants: []map[string]any{{"src": []string{"tag:twd"}, "dst": []string{"tag:twd"}, "ip": []string{"*"}, "srcPosture": []string{"posture:x"}}}},
	}
	for name, s := range cases {
		err := Check(s, Options{Headscale: true, AllowSrcs: []string{"autogroup:admin"}})
		if err == nil {
			t.Errorf("%s: Check accepted %+v", name, s)
		}
	}
	// Without the profile, tag:twd still needs an owner.
	if err := Check(Snippet{TagOwners: map[string][]string{"tag:twd": {}}}, Options{}); err == nil || !strings.Contains(err.Error(), "no owners") {
		t.Errorf("tailscale profile accepted an ownerless tag:twd: %v", err)
	}
}
