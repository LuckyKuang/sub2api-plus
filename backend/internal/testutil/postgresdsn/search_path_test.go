//go:build unit || !integration

package postgresdsn

import (
	"net/url"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestWithSearchPathURLPreservesConnectionOptions(t *testing.T) {
	for _, scheme := range []string{"postgres", "postgresql"} {
		t.Run(scheme, func(t *testing.T) {
			result, err := WithSearchPath(scheme+"://tester:test%20password@db.example:5433/fixtures?sslmode=disable&TimeZone=UTC&application_name=test%20runner&search_path=public&search_path=legacy", "Scoped Admin")
			require.NoError(t, err)
			parsed, err := url.Parse(result)
			require.NoError(t, err)
			require.Equal(t, scheme, parsed.Scheme)
			require.Equal(t, "tester", parsed.User.Username())
			password, present := parsed.User.Password()
			require.True(t, present)
			require.Equal(t, "test password", password)
			require.Equal(t, "db.example:5433", parsed.Host)
			require.Equal(t, "/fixtures", parsed.Path)
			require.Equal(t, url.Values{
				"sslmode":          {"disable"},
				"TimeZone":         {"UTC"},
				"application_name": {"test runner"},
				"search_path":      {`"Scoped Admin"`},
			}, parsed.Query())
		})
	}
}

func TestWithSearchPathKeywordPreservesQuotedCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, dsn, want string
	}{
		{
			name: "quoted spaces apostrophe and backslash",
			dsn:  `host=db.example dbname=fixtures user=tester password='test pass\'word\\value' TimeZone=UTC search_path=public`,
			want: `host=db.example dbname=fixtures user=tester password='test pass\'word\\value' TimeZone=UTC search_path=public search_path='"Scoped Admin"'`,
		},
		{
			name: "escaped trailing password space",
			dsn:  `host=db.example dbname=fixtures user=tester password=test\ password\ `,
			want: `host=db.example dbname=fixtures user=tester password=test\ password\  search_path='"Scoped Admin"'`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := WithSearchPath(tc.dsn, "Scoped Admin")
			require.NoError(t, err)
			require.Equal(t, tc.want, result)
			_, err = pq.NewConnector(result)
			require.NoError(t, err, "the driver must accept quoted keyword credentials without connecting")
		})
	}
}

func TestWithSearchPathQuotesOneSchemaIdentifier(t *testing.T) {
	const schema = `Tenant's\schema, "private"`
	urlDSN, err := WithSearchPath("postgres://db.example/fixtures", schema)
	require.NoError(t, err)
	parsed, err := url.Parse(urlDSN)
	require.NoError(t, err)
	require.Equal(t, `"Tenant's\schema, ""private"""`, parsed.Query().Get("search_path"))
	keywordDSN, err := WithSearchPath("host=db.example dbname=fixtures user=tester", schema)
	require.NoError(t, err)
	require.Equal(t, `host=db.example dbname=fixtures user=tester search_path='"Tenant\'s\\schema, ""private"""'`, keywordDSN)
	_, err = pq.NewConnector(keywordDSN)
	require.NoError(t, err)
}

func TestWithSearchPathRejectsInvalidInputWithoutExposingCredentials(t *testing.T) {
	for _, tc := range []struct {
		dsn, schema string
	}{
		{"", "test"},
		{" \t", "test"},
		{"host=db.example password=SECRET\x00", "test"},
		{"host=db.example password=SECRET", ""},
		{"host=db.example password=SECRET", "test\x00public"},
		{"postgres://tester:SECRET@db.example/%zz", "test"},
		{"postgres://tester:SECRET@db.example/fixtures?TimeZone=%zz", "test"},
	} {
		result, err := WithSearchPath(tc.dsn, tc.schema)
		require.Error(t, err)
		require.Empty(t, result)
		require.NotContains(t, err.Error(), "SECRET")
	}
}
