//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAccountProxySelectionConflicts(t *testing.T) {
	id := int64(7)
	zero := int64(0)
	for _, tc := range []struct {
		mode    string
		id      *int64
		random  bool
		want    string
		invalid bool
	}{
		{"", &id, false, "", false}, {"specified", &id, false, "specified", false}, {"direct", nil, false, "direct", false}, {"", nil, true, "random", false}, {"random", nil, false, "random", false}, {"least_used", nil, false, "least_used", false},
		{"random", &id, false, "", true}, {"least_used", &id, false, "", true}, {"direct", &id, false, "", true}, {"specified", &zero, false, "", true}, {"unknown", nil, false, "", true}, {"least_used", nil, true, "", true},
	} {
		got, err := NormalizeAccountProxyMode(tc.mode, tc.id, tc.random)
		if tc.invalid {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		}
	}
}
