package migrations

import (
	"strings"
	"testing"
)

func TestOpenAIGroupQuotaFollowResetMigration(t *testing.T) {
	raw, err := FS.ReadFile("258_openai_group_quota_follow_reset.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"quota_reset_source_account_id",
		"quota_reset_source_reset_at",
		"quota_reset_include_monthly",
		"quota_reset_config_version",
		"quota_follow_reset_event_id",
		"openai_oauth_weekly_reset_observations",
		"group_quota_follow_reset_events",
		"UNIQUE (group_id, config_version, upstream_reset_at)",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
}
