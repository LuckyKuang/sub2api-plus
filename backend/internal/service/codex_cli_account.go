package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/pelletier/go-toml/v2"
)

const CodexCLIConfigCredentialKey = "codex_cli"

// Initial sandbox permissions are editable. Noninteractive official exec
// requires never approvals, and the file credential store remains managed.
const DefaultCodexCLIConfigTOML = "sandbox_mode = \"danger-full-access\"\napproval_policy = \"never\"\ncli_auth_credentials_store = \"file\"\n"

const maxCodexCLIConfigBytes = 64 * 1024

type CodexCLIAccountConfiguration struct {
	Enabled    bool   `json:"enabled"`
	ConfigTOML string `json:"config_toml"`
}

type CodexCLIAuthView struct {
	AuthMode string            `json:"auth_mode"`
	Tokens   map[string]string `json:"tokens"`
}

type CodexCLIAuthStatus struct {
	HasAccessToken  bool `json:"has_access_token"`
	HasRefreshToken bool `json:"has_refresh_token"`
	HasIDToken      bool `json:"has_id_token"`
}

type CodexCLIAvailability struct {
	Available bool   `json:"available"`
	Code      string `json:"code"`
	Reason    string `json:"reason"`
}

type CodexCLIAccountView struct {
	Enabled              bool                 `json:"enabled"`
	ConfigTOML           string               `json:"config_toml"`
	Auth                 CodexCLIAuthView     `json:"auth"`
	AuthStatus           CodexCLIAuthStatus   `json:"auth_status"`
	ExpectedOwner        string               `json:"expected_owner"`
	OAuthCredentialOwner string               `json:"oauth_credential_owner"`
	Availability         CodexCLIAvailability `json:"availability"`
}

type CodexCLIAuthPatch struct {
	AuthMode *string                    `json:"auth_mode,omitempty"`
	Tokens   map[string]json.RawMessage `json:"tokens,omitempty"`
}

type CodexCLIAccountUpdate struct {
	ExpectedOwner string             `json:"expected_owner"`
	Enabled       *bool              `json:"enabled,omitempty"`
	ConfigTOML    *string            `json:"config_toml"`
	Auth          *CodexCLIAuthPatch `json:"auth,omitempty"`
}

func codexCLIConfigError(message string) error {
	return infraerrors.BadRequest("CODEX_CLI_CONFIG_INVALID", message)
}

func ValidateCodexCLIAccount(account *Account) error {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth ||
		account.IsCredentialShadow() || account.IsOpenAIAgentIdentity() || account.IsOpenAIPersonalAccessToken() {
		return infraerrors.BadRequest("CODEX_CLI_ACCOUNT_UNSUPPORTED", "Codex CLI configuration requires a credential-owning OpenAI ChatGPT OAuth account")
	}
	return nil
}

// NormalizeCodexCLIConfigTOML never includes parser diagnostics: a TOML error
// can contain a source line with credentials. Authentication, providers and
// identity declarations are owned by the gateway, not the account TOML.
func NormalizeCodexCLIConfigTOML(text string) (string, error) {
	if len(text) > maxCodexCLIConfigBytes || strings.ContainsRune(text, '\x00') {
		return "", codexCLIConfigError("Codex CLI config exceeds the supported size or contains invalid bytes")
	}
	if strings.TrimSpace(text) == "" {
		return DefaultCodexCLIConfigTOML, nil
	}
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(text), &parsed); err != nil {
		return "", codexCLIConfigError("Codex CLI config must be valid TOML")
	}
	for _, key := range []string{"profile", "profiles"} {
		if _, exists := parsed[key]; exists {
			return "", codexCLIConfigError("Codex CLI account configuration does not support profile or profiles; write settings directly in the account config.toml")
		}
	}
	if err := validateCodexCLIConfigFields(parsed); err != nil {
		return "", err
	}
	if _, exists := parsed["cli_auth_credentials_store"]; !exists {
		// A trailing root declaration after a table would belong to that table.
		// Prepend the managed setting so the original TOML remains meaningful.
		text = "cli_auth_credentials_store = \"file\"\n" + text
	}
	return text, nil
}

func validateCodexCLIConfigFields(fields map[string]any) error {
	for key, value := range fields {
		normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		// Reject invalid managed and policy declarations at every nesting level.
		switch normalized {
		case "service_tier":
			if value != "default" {
				return codexCLIConfigError("Codex CLI tasks support service_tier = default only; paid tiers cannot be verified from exec output")
			}
		case "cli_auth_credentials_store":
			if value != "file" {
				return codexCLIConfigError("Codex CLI cli_auth_credentials_store is managed and must be file")
			}
		case "sandbox_mode":
			text, ok := value.(string)
			if !ok || (text != "read-only" && text != "workspace-write" && text != "danger-full-access") {
				return codexCLIConfigError("Codex CLI sandbox_mode is invalid")
			}
		case "approval_policy":
			text, ok := value.(string)
			if !ok || text != "never" {
				return codexCLIConfigError("Codex CLI exec tasks are noninteractive and require approval_policy = never")
			}
		case "project_root_markers":
			return codexCLIConfigError("Codex CLI project configuration discovery is managed by the gateway")
		}
		switch normalized {
		case "auth", "auth_mode", "tokens", "token", "access_token", "refresh_token", "id_token", "api_key", "openai_api_key", "experimental_bearer_token",
			"model_provider", "model_providers", "openai_base_url", "chatgpt_base_url", "base_url", "http_headers", "env_http_headers", "headers",
			"originator", "user_agent", "useragent", "version", "client_id", "client_info", "client_version",
			"forced_login_method", "forced_chatgpt_workspace_id", "login", "credentials", "credential", "env", "environment", "env_vars", "env_key",
			"password", "secret", "private_key", "bearer_token", "authorization", "account_id", "chatgpt_account_id":
			return codexCLIConfigError("Codex CLI TOML cannot contain authentication, provider, environment credentials or outbound identity fields")
		}
		if strings.HasSuffix(normalized, "_token") || strings.HasSuffix(normalized, "_secret") || strings.HasSuffix(normalized, "_password") || strings.HasSuffix(normalized, "_api_key") {
			return codexCLIConfigError("Codex CLI TOML cannot contain credentials")
		}
		switch nested := value.(type) {
		case map[string]any:
			if err := validateCodexCLIConfigFields(nested); err != nil {
				return err
			}
		case []any:
			for _, item := range nested {
				if table, ok := item.(map[string]any); ok {
					if err := validateCodexCLIConfigFields(table); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// NormalizeCodexCLIAccountCredentials is shared by account create/update,
// imports, duplication, reauthorization and bulk writes. No auth JSON is ever
// retained next to the existing canonical OAuth credential fields.
func NormalizeCodexCLIAccountCredentials(platform, accountType string, credentials map[string]any) error {
	raw, supplied := credentials[CodexCLIConfigCredentialKey]
	if !supplied || raw == nil {
		return nil
	}
	if err := ValidateCodexCLIAccount(&Account{Platform: platform, Type: accountType, Credentials: credentials}); err != nil {
		return err
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return codexCLIConfigError("codex_cli must contain only enabled and config_toml")
	}
	for key := range object {
		if key != "config_toml" && key != "enabled" {
			return codexCLIConfigError("codex_cli must contain only enabled and config_toml")
		}
	}
	enabled := false
	if value, exists := object["enabled"]; exists {
		var valid bool
		enabled, valid = value.(bool)
		if !valid {
			return codexCLIConfigError("Codex CLI enabled must be a boolean")
		}
	}
	text := ""
	if value, exists := object["config_toml"]; exists {
		var valid bool
		text, valid = value.(string)
		if !valid {
			return codexCLIConfigError("Codex CLI config_toml must be a string")
		}
	}
	text, err := NormalizeCodexCLIConfigTOML(text)
	if err != nil {
		return err
	}
	credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": enabled, "config_toml": text}
	return nil
}

// ValidateCodexCLIEnablement applies to every persistence entrypoint, including
// ordinary account edits/imports. Missing runtime never prevents editing or
// disabling an already enabled account.
func ValidateCodexCLIEnablement(ctx context.Context, previous, candidate map[string]any) error {
	return validateCodexCLIEnablement(previous, candidate, func() bool {
		return codexcli.DefaultManager().Status(ctx).ExecutionAvailable
	})
}

func validateCodexCLIEnablement(previous, candidate map[string]any, runtimeAvailable func() bool) error {
	enabled := func(credentials map[string]any) bool {
		config, _ := credentials[CodexCLIConfigCredentialKey].(map[string]any)
		value, _ := config["enabled"].(bool)
		return value
	}
	if enabled(candidate) && !enabled(previous) && !runtimeAvailable() {
		return infraerrors.New(http.StatusUnprocessableEntity, "CODEX_CLI_RUNTIME_UNAVAILABLE", "verify or install the project-pinned official Codex CLI before enabling this account")
	}
	return nil
}

func CodexCLIConfigurationForAccount(account *Account) (CodexCLIAccountConfiguration, error) {
	if err := ValidateCodexCLIAccount(account); err != nil {
		return CodexCLIAccountConfiguration{}, err
	}
	copy := map[string]any{CodexCLIConfigCredentialKey: account.Credentials[CodexCLIConfigCredentialKey]}
	if err := NormalizeCodexCLIAccountCredentials(account.Platform, account.Type, copy); err != nil {
		return CodexCLIAccountConfiguration{}, err
	}
	if raw, ok := copy[CodexCLIConfigCredentialKey].(map[string]any); ok {
		enabled, _ := raw["enabled"].(bool)
		configTOML, _ := raw["config_toml"].(string)
		return CodexCLIAccountConfiguration{Enabled: enabled, ConfigTOML: configTOML}, nil
	}
	return CodexCLIAccountConfiguration{ConfigTOML: DefaultCodexCLIConfigTOML}, nil
}

// CodexCLIAccountOwner excludes unrelated editor and observation fields, so a
// normal account save cannot invalidate a pending CLI form. The actual write
// subsequently uses the existing full account/route database CAS.
func CodexCLIAccountOwner(account *Account) string {
	if account == nil {
		return ""
	}
	credentials := make(map[string]any)
	for _, key := range []string{CodexCLIConfigCredentialKey, "access_token", "refresh_token", "id_token", "auth_mode", "openai_auth_mode", "chatgpt_account_id", "chatgpt_user_id", "client_id", "expires_at", "_token_version", "agent_runtime_id", "agent_private_key"} {
		if value, present := account.Credentials[key]; present {
			credentials[key] = value
		}
	}
	var route any
	if account.Proxy != nil {
		p := account.Proxy
		route = []any{p.ID, p.Protocol, p.Host, p.Port, p.Username, p.Password, p.Status, p.ExpiresAt}
	}
	payload, err := json.Marshal([]any{account.ID, account.Platform, account.Type, account.ParentAccountID, credentials, account.ProxyID, route})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func CodexCLIAccountViewFromAccount(account *Account) (*CodexCLIAccountView, error) {
	config, err := CodexCLIConfigurationForAccount(account)
	if err != nil {
		return nil, err
	}
	return &CodexCLIAccountView{
		Enabled:    config.Enabled,
		ConfigTOML: config.ConfigTOML,
		Auth:       CodexCLIAuthView{AuthMode: "chatgpt", Tokens: map[string]string{"account_id": account.GetChatGPTAccountID()}},
		AuthStatus: CodexCLIAuthStatus{
			HasAccessToken: strings.TrimSpace(account.GetCredential("access_token")) != "", HasRefreshToken: strings.TrimSpace(account.GetCredential("refresh_token")) != "", HasIDToken: strings.TrimSpace(account.GetCredential("id_token")) != "",
		},
		ExpectedOwner:        CodexCLIAccountOwner(account),
		OAuthCredentialOwner: OAuthCredentialOwner(account),
		Availability:         CodexCLIAvailability{Available: true, Code: "exec_task_only", Reason: "This account supports the separate Codex CLI task endpoint; runtime installation is checked separately and ordinary Responses forwarding remains unchanged"},
	}, nil
}

// PrepareCodexCLIAccountUpdate validates a write-only auth patch and returns
// canonical credentials. JWT payloads are used only for consistency checks;
// they do not establish authentication or replace upstream token validation.
func PrepareCodexCLIAccountUpdate(account *Account, input CodexCLIAccountUpdate) (map[string]any, bool, error) {
	if err := ValidateCodexCLIAccount(account); err != nil {
		return nil, false, err
	}
	decodedOwner, err := hex.DecodeString(input.ExpectedOwner)
	if err != nil || len(decodedOwner) != sha256.Size || strings.ToLower(input.ExpectedOwner) != input.ExpectedOwner {
		return nil, false, infraerrors.BadRequest("CODEX_CLI_OWNER_REQUIRED", "expected_owner from the initiating CLI account form is required")
	}
	if input.ExpectedOwner != CodexCLIAccountOwner(account) {
		return nil, false, infraerrors.New(http.StatusConflict, "CODEX_CLI_OWNER_CHANGED", "account credentials, proxy or CLI configuration changed; reload the CLI account form")
	}
	if input.ConfigTOML == nil {
		return nil, false, codexCLIConfigError("config_toml is required")
	}
	text, err := NormalizeCodexCLIConfigTOML(*input.ConfigTOML)
	if err != nil {
		return nil, false, err
	}
	config, err := CodexCLIConfigurationForAccount(account)
	if err != nil {
		return nil, false, err
	}
	if input.Enabled != nil {
		config.Enabled = *input.Enabled
	}
	credentials := MergeCredentials(account.Credentials, map[string]any{CodexCLIConfigCredentialKey: map[string]any{"enabled": config.Enabled, "config_toml": text}})
	changed := false
	if input.Auth != nil {
		if input.Auth.AuthMode != nil && *input.Auth.AuthMode != "chatgpt" {
			return nil, false, infraerrors.BadRequest("CODEX_CLI_AUTH_INVALID", "auth_mode must be chatgpt")
		}
		for key, raw := range input.Auth.Tokens {
			storedKey := key
			switch key {
			case "access_token", "refresh_token", "id_token":
			case "account_id":
				storedKey = "chatgpt_account_id"
			default:
				return nil, false, infraerrors.BadRequest("CODEX_CLI_AUTH_INVALID", "auth tokens contain an unsupported field")
			}
			value := ""
			if string(raw) != "null" {
				if err := json.Unmarshal(raw, &value); err != nil || !validCodexCLIAuthValue(value) {
					return nil, false, infraerrors.BadRequest("CODEX_CLI_AUTH_INVALID", "auth fields must be nonempty strings or null to clear")
				}
			}
			if value != account.GetCredential(storedKey) {
				changed = true
			}
			// Explicit empty strings represent cleared canonical credentials, so
			// the shared merge cannot resurrect a null-cleared refresh token.
			credentials[storedKey] = value
		}
		if err := validateCodexCLIAuthOwner(account, credentials, input.Auth.Tokens); err != nil {
			return nil, false, err
		}
	}
	if changed {
		credentials["_token_version"] = time.Now().UnixMilli()
	}
	return credentials, changed, nil
}

func validCodexCLIAuthValue(value string) bool {
	if value == "" || len(value) > maxCodexCLIConfigBytes || strings.TrimSpace(value) != value || strings.Contains(value, "***") || strings.ContainsRune(value, '…') || strings.EqualFold(value, "[redacted]") || strings.EqualFold(value, "<redacted>") {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			return false
		}
	}
	return true
}

func codexCLIClaimUser(claims *openai.IDTokenClaims) string {
	if claims == nil {
		return ""
	}
	if info := claims.GetUserInfo(); info.ChatGPTUserID != "" {
		return info.ChatGPTUserID
	}
	return claims.Sub
}

func validateCodexCLIAuthOwner(account *Account, credentials map[string]any, patch map[string]json.RawMessage) error {
	invalid := func() error {
		return infraerrors.BadRequest("CODEX_CLI_AUTH_OWNER_MISMATCH", "auth changes must belong to the existing ChatGPT user and workspace")
	}
	workspace, _ := credentials["chatgpt_account_id"].(string)
	if workspace != "" && account.GetChatGPTAccountID() != "" && workspace != account.GetChatGPTAccountID() {
		return invalid()
	}
	expectedUser := account.GetCredential("chatgpt_user_id")
	for _, key := range []string{"id_token", "access_token"} {
		if expectedUser != "" {
			break
		}
		// Imported or explicitly edited accounts may have no retained ID token
		// or user metadata. Their existing access-token principal still owns the
		// grant; a shared workspace alone cannot authorize a different user.
		if old, err := openai.DecodeIDToken(account.GetCredential(key)); err == nil {
			expectedUser = codexCLIClaimUser(old)
		}
	}
	for _, key := range []string{"access_token", "id_token"} {
		if _, supplied := patch[key]; !supplied {
			continue
		}
		value, _ := credentials[key].(string)
		if value == "" {
			if key == "access_token" {
				credentials["expires_at"] = ""
			}
			continue
		}
		claims, err := openai.DecodeIDToken(value)
		if err != nil || claims.OpenAIAuth == nil || codexCLIClaimUser(claims) == "" {
			return invalid()
		}
		if expectedUser != "" && expectedUser != codexCLIClaimUser(claims) {
			return invalid()
		}
		expectedUser = codexCLIClaimUser(claims)
		// ID tokens may retain the original workspace after official workspace
		// switching. Only a matching user and an unchanged retained ID workspace
		// can accompany the selected access-token workspace.
		claimWorkspace := strings.TrimSpace(claims.OpenAIAuth.ChatGPTAccountID)
		if claimWorkspace == "" || (workspace != "" && claimWorkspace != workspace) {
			old, oldErr := openai.DecodeIDToken(account.GetCredential("id_token"))
			if key != "id_token" || oldErr != nil || old.OpenAIAuth == nil || old.OpenAIAuth.ChatGPTAccountID != claimWorkspace || codexCLIClaimUser(old) != expectedUser {
				return invalid()
			}
		}
		if workspace == "" {
			workspace = claimWorkspace
			credentials["chatgpt_account_id"] = workspace
		}
		credentials["chatgpt_user_id"] = expectedUser
		if key == "access_token" {
			if claims.Exp <= time.Now().Unix() {
				return infraerrors.BadRequest("CODEX_CLI_AUTH_INVALID", "the replacement access token is expired or has no expiry")
			}
			credentials["expires_at"] = time.Unix(claims.Exp, 0).UTC().Format(time.RFC3339)
		}
	}
	if workspace == "" {
		for _, key := range []string{"access_token", "id_token", "refresh_token"} {
			if value, _ := credentials[key].(string); value != "" {
				return invalid()
			}
		}
	}
	return nil
}

// BuildCodexCLIAuthJSON renders a static execution credential from one acquired
// owner. Native ChatGPT auth independently fetches cloud configuration and can
// refresh grants even with a custom provider. Official API-key file mode treats
// the mapped OAuth access token as opaque; refresh/ID tokens remain exclusively
// in the gateway's canonical store and never enter the CLI process or files.
func BuildCodexCLIAuthJSON(account *Account) ([]byte, error) {
	if err := ValidateCodexCLIAccount(account); err != nil {
		return nil, err
	}
	access := strings.TrimSpace(account.GetCredential("access_token"))
	workspace := strings.TrimSpace(account.GetChatGPTAccountID())
	if access == "" || workspace == "" {
		return nil, infraerrors.BadRequest("CODEX_CLI_AUTH_INCOMPLETE", "Codex CLI execution requires an access token and ChatGPT workspace")
	}
	return json.Marshal(map[string]any{"auth_mode": "apikey", "OPENAI_API_KEY": access})
}
