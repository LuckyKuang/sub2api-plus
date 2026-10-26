package zcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

// ProjectAccessToken is ephemeral; persist its public Key ID and scope only.
type ProjectAccessToken struct {
	Token     string
	ExpiresAt time.Time
}

type ProjectTokenHTTPError struct{ Status int }

func (e *ProjectTokenHTTPError) Error() string { return fmt.Sprintf("project token HTTP %d", e.Status) }
func (e *ProjectTokenHTTPError) Unwrap() error { return &BusinessHTTPError{Status: e.Status} }
func IsTransientProjectTokenError(err error) bool {
	var httpErr *BusinessHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == 429 || httpErr.Status >= 500
	}
	var netErr interface{ Timeout() bool }
	return errors.As(err, &netErr)
}

func (c *CredentialClient) IssueProjectAccessToken(ctx context.Context, provider, login, organization, project, keyID string) (*ProjectAccessToken, error) {
	ctx = withIdentity(ctx)
	if NormalizeProvider(provider) == "" || strings.TrimSpace(login) == "" || organization == "" || project == "" || keyID == "" {
		return nil, errors.New("project token scope is incomplete")
	}
	identity, _ := outboundidentity.FromContext(ctx)
	body, _ := json.Marshal(map[string]string{"clientType": "zcode", "clientVersion": identity.Version})
	var payload struct {
		Code    json.RawMessage `json:"code"`
		Success *bool           `json:"success"`
		Data    *struct {
			Enable    *bool   `json:"enable"`
			Token     string  `json:"accessToken"`
			TokenType string  `json:"tokenType"`
			ExpiresIn float64 `json:"expiresIn"`
			ExpiresAt float64 `json:"expiresAt"`
		} `json:"data"`
	}
	start := time.Now()
	err := c.doJSON(ctx, http.MethodPost, ProjectAPIKeysURL(c.origin(provider), organization, project)+"/"+url.PathEscape(keyID)+"/access_tokens", map[string]string{"Authorization": "Bearer " + strings.TrimSpace(login), "Content-Type": "application/json"}, body, &payload)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	code := strings.TrimSpace(string(payload.Code))
	if code == "404" || code == `"404"` {
		return nil, &BusinessHTTPError{Status: http.StatusNotFound}
	}
	if (code != "0" && code != "200" && code != `"0"` && code != `"200"`) || (payload.Success != nil && !*payload.Success) || payload.Data == nil {
		return nil, errors.New("project token issuance rejected")
	}
	data := payload.Data
	if data.Enable != nil && !*data.Enable {
		return nil, errors.New("project token disabled")
	}
	if strings.TrimSpace(data.Token) == "" || data.TokenType != "Bearer" || data.ExpiresIn <= 0 || data.ExpiresIn > 365*24*3600 ||
		data.ExpiresAt <= 0 || math.IsNaN(data.ExpiresAt) || math.IsInf(data.ExpiresAt, 0) {
		return nil, errors.New("invalid project token response")
	}
	for _, r := range data.Token {
		if r <= 32 || r > 126 {
			return nil, errors.New("invalid project token response")
		}
	}
	expiry := start.Add(time.Duration(data.ExpiresIn * float64(time.Second)))
	// Compare before conversion: a finite, very distant absolute expiry must
	// not overflow int64 and shorten the valid relative lifetime accidentally.
	if data.ExpiresAt < float64(expiry.Unix())+float64(expiry.Nanosecond())/float64(time.Second) {
		seconds, fraction := math.Modf(data.ExpiresAt)
		expiry = time.Unix(int64(seconds), int64(fraction*float64(time.Second)))
	}
	if !time.Now().Before(expiry) {
		return nil, errors.New("project token expired")
	}
	return &ProjectAccessToken{Token: data.Token, ExpiresAt: expiry}, nil
}
