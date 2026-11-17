//go:build unit

package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Plan cards list one feature per line; a stored value must never be shown as
// raw JSON text.
func TestParseFeatures(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: []string{}},
		{name: "one per line with blanks and padding", raw: " Daily $15 \n\n Weekly $75\n", want: []string{"Daily $15", "Weekly $75"}},
		{name: "json string array", raw: `["每日 $15 额度","每周 $75 额度","30 天有效期"]`, want: []string{"每日 $15 额度", "每周 $75 额度", "30 天有效期"}},
		{name: "json array with blank element", raw: ` ["a", " ", "b"] `, want: []string{"a", "b"}},
		{name: "bracketed text that is not a json array stays a line", raw: "[beta] early access\nPriority queue", want: []string{"[beta] early access", "Priority queue"}},
		{name: "empty json array", raw: "[]", want: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, parseFeatures(tc.raw))
		})
	}
}
