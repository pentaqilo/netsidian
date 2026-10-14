package authpolicy

import (
	"encoding/json"
	"testing"
)

func TestClaimRulesDenyMissingMalformedAndPartialMatches(t *testing.T) {
	rules, err := Parse(`{"/realm_access/roles":["writer","admin"],"/tenant":["team-a"]}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		claims string
		allow  bool
	}{
		{`{"realm_access":{"roles":["admin"]},"tenant":"team-a"}`, true},
		{`{"realm_access":{"roles":"writer"},"tenant":"team-a"}`, true},
		{`{"realm_access":{"roles":["administrator"]},"tenant":"team-a"}`, false},
		{`{"realm_access":{"roles":["admin"]},"tenant":"team-b"}`, false},
		{`{"realm_access":{"roles":["admin",42]},"tenant":"team-a"}`, false},
		{`{"realm_access":{"roles":["admin"]}}`, false},
		{`{"tenant":"team-a"}`, false},
	} {
		var claims map[string]any
		if err = json.Unmarshal([]byte(test.claims), &claims); err != nil {
			t.Fatal(err)
		}
		if got := rules.Match(claims); got != test.allow {
			t.Errorf("%s: allow=%v want=%v", test.claims, got, test.allow)
		}
	}
	if (Rules(nil)).Match(map[string]any{}) {
		t.Fatal("empty rules must never grant a role")
	}
}

func TestClaimRulesConfiguration(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"groups":["admin"]}`, `{"/groups":[]}`, `{"/groups":[""]}`, `{"/groups":[true]}`, `{"/bad~path":["x"]}`} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("accepted invalid policy %s", raw)
		}
	}
	rules, err := Parse(`{"/https:~1~1example.test~1roles":["admin"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !rules.Match(map[string]any{"https://example.test/roles": "admin"}) {
		t.Fatal("JSON Pointer escaping did not match")
	}
}
