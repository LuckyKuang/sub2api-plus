package zcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Business origins. The ZCode client keeps the login/business origin and the
// model origin separate: BigModel business calls go to bigmodel.cn while model
// traffic goes to open.bigmodel.cn.
const (
	DefaultBigModelBusinessOrigin = "https://bigmodel.cn"
	DefaultZaiBusinessOrigin      = "https://api.z.ai"

	EnvBigModelBusinessOrigin = "ZHIPU_BIGMODEL_ORIGIN"
	EnvZaiBusinessOrigin      = "ZHIPU_ZAI_ORIGIN"

	customerInfoPath      = "/api/biz/customer/getCustomerInfo"
	zaiBusinessLoginPath  = "/api/auth/z/login"
	projectAPIKeysPathFmt = "/api/biz/v1/organization/%s/projects/%s/api_keys"
)

// Published API-key names used by the official client. The personal coding-plan
// key and the team-plan project key are distinct resources.
const (
	PersonalAPIKeyName = "zcode-api-key"
	TeamAPIKeyName     = "zcode-team-api-key"
	TeamAPIKeyType     = 2
)

// ZaiBusinessTokenEnv and friends are documented for operators; the values are
// only used through the resolvers below.
const CredentialRequestTimeout = 15 * time.Second

// CodingPlanCredential is a resolved model credential plus the project scope it
// belongs to. Team-plan traffic must also send the organization and project
// headers, so the scope is returned rather than only the key.
type CodingPlanCredential struct {
	APIKey         string
	OrganizationID string
	ProjectID      string
}

// CredentialClient resolves the model credentials an account-link flow needs.
// Every call is a ZCode platform business API call and carries the same trusted
// identity as the rest of the provider traffic.
type CredentialClient struct {
	client         HTTPDoer
	bigModelOrigin string
	zaiOrigin      string
	timeout        time.Duration
}

// NewCredentialClient builds a credential resolver. Empty origins fall back to
// the published defaults; a nil doer falls back to the default HTTP client.
func NewCredentialClient(client HTTPDoer, bigModelOrigin, zaiOrigin string) *CredentialClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &CredentialClient{
		client:         client,
		bigModelOrigin: normalizeOrigin(bigModelOrigin, DefaultBigModelBusinessOrigin),
		zaiOrigin:      normalizeOrigin(zaiOrigin, DefaultZaiBusinessOrigin),
		timeout:        CredentialRequestTimeout,
	}
}

// BigModelOrigin returns the effective BigModel business origin.
func (c *CredentialClient) BigModelOrigin() string {
	if c == nil {
		return DefaultBigModelBusinessOrigin
	}
	return c.bigModelOrigin
}

// ZaiOrigin returns the effective Z.ai business origin.
func (c *CredentialClient) ZaiOrigin() string {
	if c == nil {
		return DefaultZaiBusinessOrigin
	}
	return c.zaiOrigin
}

// ResolveBusinessOrigins reads the optional origin overrides from the
// environment. The defaults are the published production origins.
func ResolveBusinessOrigins() (bigModelOrigin, zaiOrigin string) {
	return normalizeOrigin(os.Getenv(EnvBigModelBusinessOrigin), DefaultBigModelBusinessOrigin),
		normalizeOrigin(os.Getenv(EnvZaiBusinessOrigin), DefaultZaiBusinessOrigin)
}

// ResolveHandshakeBaseURL reads the optional handshake override.
func ResolveHandshakeBaseURL() string {
	return normalizeOrigin(os.Getenv(EnvHandshakeBaseURL), DefaultHandshakeBaseURL)
}

func normalizeOrigin(origin, fallback string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		return fallback
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fallback
	}
	return origin
}

func (c *CredentialClient) origin(provider string) string {
	if NormalizeProvider(provider) == ProviderZai {
		return c.zaiOrigin
	}
	return c.bigModelOrigin
}

// bizHeader builds the Authorization header for one business call.
//
// The official client is not internally consistent for the Z.ai estate: the
// personal coding-plan key derivation sends `Bearer <token>` while the
// subscription and team-plan paths send the bare token. The resolvers below
// mirror each call site exactly rather than normalizing, so a future upstream
// change on either path can be matched independently.
func bizAuthorization(provider, token string, bearer bool) string {
	token = strings.TrimSpace(token)
	if NormalizeProvider(provider) == ProviderZai && bearer {
		return "Bearer " + token
	}
	return token
}

// ExchangeZaiBusinessToken converts the Z.ai OAuth access token returned by the
// handshake into the business token every Z.ai business and model call expects.
// BigModel needs no such exchange.
func (c *CredentialClient) ExchangeZaiBusinessToken(ctx context.Context, oauthAccessToken string) (string, error) {
	ctx = withIdentity(ctx)
	oauthAccessToken = strings.TrimSpace(oauthAccessToken)
	if oauthAccessToken == "" {
		return "", fmt.Errorf("zai oauth access token is required")
	}
	body, err := json.Marshal(map[string]string{"token": oauthAccessToken})
	if err != nil {
		return "", err
	}
	var payload struct {
		Code    json.RawMessage `json:"code"`
		Success *bool           `json:"success"`
		Data    *struct {
			AccessToken      string `json:"access_token"`
			AccessTokenCamel string `json:"accessToken"`
			ExpiresIn        int64  `json:"expires_in"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, c.zaiOrigin+zaiBusinessLoginPath, map[string]string{
		"Content-Type": "application/json",
	}, body, &payload); err != nil {
		return "", err
	}
	if !bizCodeAccepted(payload.Code, payload.Success, payload.Data != nil) {
		return "", fmt.Errorf("zai business login rejected the oauth token")
	}
	if payload.Data == nil {
		return "", fmt.Errorf("zai business login returned no data")
	}
	accessToken := strings.TrimSpace(payload.Data.AccessToken)
	if accessToken == "" {
		accessToken = strings.TrimSpace(payload.Data.AccessTokenCamel)
	}
	if accessToken == "" {
		return "", fmt.Errorf("zai business login returned no access token")
	}
	return accessToken, nil
}

// ResolveIndividualCodingPlanKey derives the personal coding-plan API key for a
// linked account. It mirrors the official client: read the customer profile,
// select a non-team organization/project, find or create the published personal
// key, then copy its secret and join both halves.
func (c *CredentialClient) ResolveIndividualCodingPlanKey(ctx context.Context, provider, accessToken string) (*CodingPlanCredential, error) {
	ctx = withIdentity(ctx)
	provider = NormalizeProvider(provider)
	if provider == "" {
		return nil, fmt.Errorf("unsupported oauth provider")
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	origin := c.origin(provider)
	authorization := bizAuthorization(provider, accessToken, true)

	customer, err := c.fetchCustomerInfo(ctx, origin, authorization, nil)
	if err != nil {
		return nil, err
	}
	organizationID, projectID := PickPersonalScope(customer)
	if organizationID == "" || projectID == "" {
		return nil, fmt.Errorf("no personal organization or project is available for this account")
	}
	listURL := ProjectAPIKeysURL(origin, organizationID, projectID)

	apiKey, err := c.findOrCreateAPIKey(ctx, listURL, authorization, nil, PersonalAPIKeyName, nil)
	if err != nil {
		return nil, err
	}
	secret, err := c.copyAPIKeySecret(ctx, listURL, authorization, nil, apiKey)
	if err != nil {
		return nil, err
	}
	if secret == "" {
		// Z.ai requires the copied secret; BigModel historically accepted the bare
		// key, so the two estates differ in how hard the failure is.
		if provider == ProviderZai {
			return nil, fmt.Errorf("zai copy endpoint returned no secret key")
		}
		return &CodingPlanCredential{APIKey: apiKey, OrganizationID: organizationID, ProjectID: projectID}, nil
	}
	return &CodingPlanCredential{
		APIKey:         apiKey + "." + secret,
		OrganizationID: organizationID,
		ProjectID:      projectID,
	}, nil
}

// ResolveTeamPlanKey derives the team-plan project key for a linked account. The
// team scope is supplied by the operator (or discovered at link time) because a
// team account can own several projects, each with its own project key.
func (c *CredentialClient) ResolveTeamPlanKey(ctx context.Context, provider, accessToken, organizationID, projectID string) (*CodingPlanCredential, error) {
	ctx = withIdentity(ctx)
	provider = NormalizeProvider(provider)
	if provider == "" {
		return nil, fmt.Errorf("unsupported oauth provider")
	}
	accessToken = strings.TrimSpace(accessToken)
	organizationID, projectID = strings.TrimSpace(organizationID), strings.TrimSpace(projectID)
	if accessToken == "" || organizationID == "" || projectID == "" {
		return nil, fmt.Errorf("team plan requires an access token, organization and project")
	}
	origin := c.origin(provider)
	authorization := bizAuthorization(provider, accessToken, false)
	teamHeaders := map[string]string{
		"bigmodel-organization": organizationID,
		"bigmodel-project":      projectID,
	}

	// The scope must exist for this account before a project key can be ensured.
	customer, err := c.fetchCustomerInfo(ctx, origin, authorization, nil)
	if err != nil {
		return nil, err
	}
	if !HasProject(customer, organizationID, projectID) {
		return nil, fmt.Errorf("team project is not visible to this account")
	}

	listURL := ProjectAPIKeysURL(origin, organizationID, projectID)
	keyType := TeamAPIKeyType
	apiKey, err := c.findOrCreateAPIKey(ctx, listURL, authorization, teamHeaders, TeamAPIKeyName, &keyType)
	if err != nil {
		return nil, err
	}
	secret, err := c.copyAPIKeySecret(ctx, listURL, authorization, teamHeaders, apiKey)
	if err != nil {
		return nil, err
	}
	key := apiKey
	if secret != "" {
		key = apiKey + "." + secret
	}
	return &CodingPlanCredential{APIKey: key, OrganizationID: organizationID, ProjectID: projectID}, nil
}

// ProjectAPIKeysURL builds the project API-key collection URL.
func ProjectAPIKeysURL(origin, organizationID, projectID string) string {
	return origin + fmt.Sprintf(projectAPIKeysPathFmt, url.PathEscape(organizationID), url.PathEscape(projectID))
}

func (c *CredentialClient) fetchCustomerInfo(ctx context.Context, origin, authorization string, extra map[string]string) (*CustomerInfo, error) {
	var payload struct {
		Code    json.RawMessage `json:"code"`
		Success *bool           `json:"success"`
		Data    *CustomerInfo   `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, origin+customerInfoPath, mergeHeaders(map[string]string{
		"Authorization": authorization,
	}, extra), nil, &payload); err != nil {
		return nil, err
	}
	if !bizCodeAccepted(payload.Code, payload.Success, payload.Data != nil) || payload.Data == nil {
		return nil, fmt.Errorf("customer lookup was rejected for this account")
	}
	return payload.Data, nil
}

func (c *CredentialClient) findOrCreateAPIKey(ctx context.Context, listURL, authorization string, extra map[string]string, name string, keyType *int) (string, error) {
	headers := mergeHeaders(map[string]string{"Authorization": authorization}, extra)
	keys, err := c.listAPIKeys(ctx, listURL, headers)
	if err != nil {
		return "", err
	}
	if key := matchAPIKey(keys, name, keyType); key != "" {
		return key, nil
	}
	createBody := map[string]any{"name": name}
	if keyType != nil {
		createBody["keyType"] = *keyType
	}
	body, err := json.Marshal(createBody)
	if err != nil {
		return "", err
	}
	var payload struct {
		Code    json.RawMessage `json:"code"`
		Success *bool           `json:"success"`
		Data    *APIKeySummary  `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, listURL, mergeHeaders(map[string]string{
		"Authorization": authorization,
		"Content-Type":  "application/json",
	}, extra), body, &payload); err != nil {
		return "", err
	}
	if !bizCodeAccepted(payload.Code, payload.Success, payload.Data != nil) {
		return "", fmt.Errorf("api key creation was rejected for this account")
	}
	if payload.Data == nil || strings.TrimSpace(payload.Data.APIKey) == "" {
		return "", fmt.Errorf("api key creation returned no key")
	}
	return strings.TrimSpace(payload.Data.APIKey), nil
}

func (c *CredentialClient) listAPIKeys(ctx context.Context, listURL string, headers map[string]string) ([]APIKeySummary, error) {
	var payload struct {
		Code    json.RawMessage `json:"code"`
		Success *bool           `json:"success"`
		Data    []APIKeySummary `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, listURL, headers, nil, &payload); err != nil {
		return nil, err
	}
	if !bizCodeAccepted(payload.Code, payload.Success, payload.Data != nil) {
		return nil, fmt.Errorf("api key listing was rejected for this account")
	}
	return payload.Data, nil
}

func (c *CredentialClient) copyAPIKeySecret(ctx context.Context, listURL, authorization string, extra map[string]string, apiKey string) (string, error) {
	target := listURL + "/copy/" + url.PathEscape(apiKey)
	var payload struct {
		Code    json.RawMessage `json:"code"`
		Success *bool           `json:"success"`
		Data    *struct {
			SecretKey string `json:"secretKey"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, target, mergeHeaders(map[string]string{
		"Authorization": authorization,
	}, extra), nil, &payload); err != nil {
		return "", err
	}
	if !bizCodeAccepted(payload.Code, payload.Success, payload.Data != nil) || payload.Data == nil {
		return "", nil
	}
	return strings.TrimSpace(payload.Data.SecretKey), nil
}

// doJSON performs one business call and decodes the JSON body into dest. A nil
// dest discards the body.
func (c *CredentialClient) doJSON(ctx context.Context, method, target string, headers map[string]string, body []byte, dest any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = CredentialRequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Accept", "application/json")
	prepareRequest(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("business endpoint returned status %d", resp.StatusCode)
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("decode business response: %w", err)
	}
	return nil
}

func mergeHeaders(base, extra map[string]string) map[string]string {
	if len(extra) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(extra))
	for name, value := range base {
		merged[name] = value
	}
	for name, value := range extra {
		merged[name] = value
	}
	return merged
}

// bizCodeAccepted mirrors the official client's envelope rule: an explicit
// success=false is a failure, an explicit numeric code must be 0 or 200, and an
// envelope with no code at all is accepted only when it carries data.
func bizCodeAccepted(code json.RawMessage, success *bool, hasData bool) bool {
	if success != nil && !*success {
		return false
	}
	trimmed := strings.TrimSpace(string(code))
	if trimmed == "" || trimmed == "null" {
		return (success != nil && *success) || hasData
	}
	if trimmed == "0" || trimmed == "200" || trimmed == `"0"` || trimmed == `"200"` {
		return true
	}
	if value, err := strconv.Atoi(strings.Trim(trimmed, `"`)); err == nil {
		return value == 0 || value == 200
	}
	return false
}

// CustomerInfo is the customer profile returned by the business API.
type CustomerInfo struct {
	Organizations []OrganizationInfo `json:"organizations"`
}

// OrganizationInfo is one organization with its visible projects.
type OrganizationInfo struct {
	OrganizationID   string        `json:"organizationId"`
	OrganizationName string        `json:"organizationName"`
	Projects         []ProjectInfo `json:"projects"`
}

// ProjectInfo is one project. projectType "2" marks a team project, which the
// personal key derivation must skip.
type ProjectInfo struct {
	ProjectID   string          `json:"projectId"`
	ProjectName string          `json:"projectName"`
	ProjectType json.RawMessage `json:"projectType"`
}

// APIKeySummary is one project API-key entry.
type APIKeySummary struct {
	APIKey  string `json:"apiKey"`
	KeyType *int   `json:"keyType"`
	Name    string `json:"name"`
}

// Personal scope selection names, matching the official client.
const (
	defaultOrganizationName = "默认机构"
	defaultProjectName      = "默认项目"
	teamProjectType         = "2"
)

// PickPersonalScope selects the non-team organization/project the personal key
// belongs to. The official client prefers the default-named organization and
// project and otherwise takes the first eligible entry.
func PickPersonalScope(info *CustomerInfo) (organizationID, projectID string) {
	if info == nil {
		return "", ""
	}
	type candidate struct {
		organizationID   string
		organizationName string
		projectID        string
		projectName      string
	}
	var candidates []candidate
	for _, organization := range info.Organizations {
		organizationID := strings.TrimSpace(organization.OrganizationID)
		if organizationID == "" {
			continue
		}
		for _, project := range organization.Projects {
			if strings.TrimSpace(string(project.ProjectType)) == teamProjectType ||
				strings.Trim(strings.TrimSpace(string(project.ProjectType)), `"`) == teamProjectType {
				continue
			}
			projectID := strings.TrimSpace(project.ProjectID)
			if projectID == "" {
				continue
			}
			candidates = append(candidates, candidate{
				organizationID:   organizationID,
				organizationName: organization.OrganizationName,
				projectID:        projectID,
				projectName:      project.ProjectName,
			})
		}
	}
	if len(candidates) == 0 {
		return "", ""
	}
	selected := candidates[0]
	for _, item := range candidates {
		if strings.Contains(item.organizationName, defaultOrganizationName) {
			selected = item
			break
		}
	}
	for _, item := range candidates {
		if item.organizationID != selected.organizationID {
			continue
		}
		if strings.Contains(item.projectName, defaultProjectName) {
			selected = item
			break
		}
	}
	return selected.organizationID, selected.projectID
}

// HasProject reports whether the account can see the given team project.
func HasProject(info *CustomerInfo, organizationID, projectID string) bool {
	if info == nil {
		return false
	}
	for _, organization := range info.Organizations {
		if strings.TrimSpace(organization.OrganizationID) != organizationID {
			continue
		}
		for _, project := range organization.Projects {
			if strings.TrimSpace(project.ProjectID) == projectID {
				return true
			}
		}
	}
	return false
}

// matchAPIKey finds the published key entry, optionally constrained by keyType.
func matchAPIKey(keys []APIKeySummary, name string, keyType *int) string {
	for _, entry := range keys {
		if strings.TrimSpace(entry.Name) != name {
			continue
		}
		if keyType != nil {
			if entry.KeyType == nil || *entry.KeyType != *keyType {
				continue
			}
		}
		if apiKey := strings.TrimSpace(entry.APIKey); apiKey != "" {
			return apiKey
		}
	}
	return ""
}
