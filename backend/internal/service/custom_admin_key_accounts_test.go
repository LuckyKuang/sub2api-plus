//go:build unit

package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/adminaccess"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestScopedAdminAccountFieldsRejectUnknown(t *testing.T) {
	for _, body := range []string{`{"credentials":{}}`, `{"extra":{}}`, `{"platform":"openai"}`} {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal([]byte(body), &fields)
		if _, err := scopedAccountFieldPermissions(fields, false); err == nil {
			t.Fatalf("应拒绝字段: %s", body)
		}
	}
}

func TestScopedAdminAccountModelMappingFieldPermissions(t *testing.T) {
	for _, create := range []bool{false, true} {
		for _, mapping := range []string{`{"gpt-6":"gpt-6","alias":"gpt-6"}`, `{}`, `null`} {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(`{"credentials":{"model_mapping":`+mapping+`}}`), &fields))
			permissions, err := scopedAccountFieldPermissions(fields, create)
			require.NoError(t, err)
			require.Equal(t, []string{"accounts.models.write"}, permissions)
		}
	}
	for _, mapping := range []string{`[]`, `"gpt-6"`, `{"gpt-6":42}`, `{"gpt-6":null}`, `{"gpt-6":{}}`, `{"":"gpt-6"}`, `{"gpt-6":" "}`, `{"gpt-6":"` + strings.Repeat("x", 65536) + `"}`} {
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(`{"credentials":{"model_mapping":`+mapping+`}}`), &fields))
		_, err := scopedAccountFieldPermissions(fields, false)
		require.Equal(t, 400, infraerrors.Code(err), mapping)
	}
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(`{"credentials":{"model_mapping":{},"access_token":"secret","base_url":"https://example.com"}}`), &fields))
	permissions, err := scopedAccountFieldPermissions(fields, false)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"accounts.models.write", "accounts.credentials", "accounts.network"}, permissions)
}

func TestScopedAdminGroupReplacementPreservesForeignBindings(t *testing.T) {
	policy := adminaccess.Policy{Rules: []adminaccess.Rule{{Permissions: []string{"accounts.groups"}, GroupIDs: []int64{1}, AccountTypes: []string{"oauth"}}}}
	resource := adminaccess.Account{ID: 9, Type: "oauth", GroupIDs: []int64{1}}
	for _, tc := range []struct {
		request []int64
		want    []int64
		denied  bool
	}{{[]int64{1}, []int64{2, 1}, false}, {[]int64{}, []int64{2}, false}, {[]int64{2}, nil, true}} {
		got, err := scopedAdminMergeGroups(policy, resource, []int64{1, 2}, tc.request)
		if (err != nil) != tc.denied || (!tc.denied && !reflect.DeepEqual(got, tc.want)) {
			t.Fatalf("request=%v got=%v err=%v", tc.request, got, err)
		}
	}
}

func TestScopedAdminAccountCredentialsNeedIndependentPermissions(t *testing.T) {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"credentials":{"api_key":"secret","base_url":"https://example.com"},"status":"active","concurrency":2}`), &fields)
	permissions, err := scopedAccountFieldPermissions(fields, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"accounts.credentials", "accounts.network", "accounts.status", "accounts.schedule"} {
		found := false
		for _, p := range permissions {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("缺少独立权限 %s", want)
		}
	}
}
