//go:build unit

package service

import "testing"

func TestMatchUpstreamRefusal(t *testing.T) {
	cfg := DefaultUpstreamRefusalGuardConfig()
	if cfg.Enabled || cfg.UserThreshold != 3 || cfg.CountWindowSeconds != 300 || cfg.UserCooldownSeconds != 300 || cfg.SessionBlockTTLSeconds != 2592000 {
		t.Fatal("incorrect defaults")
	}
	cfg.Enabled = true
	cases := []struct {
		body   string
		status int
		want   string
	}{
		{`{"error":{"type":"policy_blocked","code":"refusal_guard","message":"Do not retry"}}`, 400, "vm2api_refusal_guard"},
		{`{"type":"error","error":{"type":"invalid_request_error","message":"Request appears to violate our Usage Policy"}}`, 400, "anthropic_usage_policy"},
		{`{"error":{"type":"invalid_request_error","message":"bad parameter"}}`, 400, ""},
		{`{"error":{"code":"refusal_guard"}}`, 500, ""},
	}
	for _, tc := range cases {
		id, ok := MatchUpstreamRefusal(cfg, tc.status, []byte(tc.body))
		if id != tc.want || ok != (tc.want != "") {
			t.Errorf("body %s: %s %v", tc.body, id, ok)
		}
	}
	cfg.Enabled = false
	if _, ok := MatchUpstreamRefusal(cfg, 400, []byte(cases[0].body)); ok {
		t.Fatal("disabled config matched")
	}
	cfg.Enabled = true
	cfg.Rules = []UpstreamRefusalRule{{ID: "custom", Enabled: true, MessageKeywords: []string{"custom policy"}}}
	if id, ok := MatchUpstreamRefusal(cfg, 403, []byte(`{"message":"CUSTOM POLICY"}`)); !ok || id != "custom" {
		t.Fatal("custom rule failed")
	}
}
func TestUpstreamRefusalGuardConfigValidation(t *testing.T) {
	cfg := DefaultUpstreamRefusalGuardConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.UserThreshold = 0
	if cfg.Validate() == nil {
		t.Fatal("zero threshold accepted")
	}
	cfg.UserThreshold = 3
	cfg.Rules = []UpstreamRefusalRule{{ID: "invalid", Enabled: true}}
	if cfg.Validate() == nil {
		t.Fatal("empty rule accepted")
	}
}
