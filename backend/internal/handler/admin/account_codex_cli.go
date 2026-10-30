package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) GetCodexCLIAccount(c *gin.Context) {
	account, ok := h.codexCLIAccount(c)
	if !ok {
		return
	}
	view, err := service.CodexCLIAccountViewFromAccount(account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, view)
}

func (h *AccountHandler) UpdateCodexCLIAccount(c *gin.Context) {
	account, ok := h.codexCLIAccount(c)
	if !ok {
		return
	}
	expected, err := service.CaptureOAuthCredentialOwner(account)
	if err != nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("CODEX_CLI_ACCOUNT_UNAVAILABLE", "account configuration is unavailable"))
		return
	}
	// Strict, bounded decoding prevents silently accepting nested auth copies or
	// leaking TOML/token source snippets through binding errors.
	var input service.CodexCLIAccountUpdate
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 512*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("CODEX_CLI_REQUEST_INVALID", "invalid Codex CLI account configuration request"))
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.ErrorFrom(c, infraerrors.BadRequest("CODEX_CLI_REQUEST_INVALID", "exactly one Codex CLI account configuration is required"))
		return
	}
	credentials, authChanged, err := service.PrepareCodexCLIAccountUpdate(expected, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := service.ValidateCodexCLIEnablement(c.Request.Context(), expected.Credentials, credentials); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var updated *service.Account
	if expected.Type == service.AccountTypeAPIKey {
		committer, ok := h.adminService.(service.CodexCLIAPIKeyConfigurationCommitter)
		if !ok {
			response.ErrorFrom(c, infraerrors.ServiceUnavailable("CODEX_CLI_PERSISTENCE_UNAVAILABLE", "atomic CLI configuration persistence is unavailable"))
			return
		}
		updated, err = committer.CommitCodexCLIAPIKeyConfiguration(c.Request.Context(), expected, credentials)
	} else {
		committer, ok := h.adminService.(service.OAuthCredentialCommitter)
		if !ok {
			response.ErrorFrom(c, infraerrors.ServiceUnavailable("OAUTH_PERSISTENCE_UNAVAILABLE", "atomic OAuth credential persistence is unavailable"))
			return
		}
		updated, err = committer.CommitOAuthCredentials(c.Request.Context(), expected, expected.Type, credentials, nil, false)
	}

	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if authChanged && h.tokenCacheInvalidator != nil {
		if err := h.tokenCacheInvalidator.InvalidateToken(c.Request.Context(), updated); err != nil {
			response.ErrorFrom(c, infraerrors.ServiceUnavailable("CODEX_CLI_TOKEN_INVALIDATION_FAILED", "account configuration was saved but cached credentials could not be invalidated; reload before retrying"))
			return
		}
	}
	view, err := service.CodexCLIAccountViewFromAccount(updated)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, view)
}

func (h *AccountHandler) codexCLIAccount(c *gin.Context) (*service.Account, bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return nil, false
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return nil, false
	}
	if err := service.ValidateCodexCLIAccount(account); err != nil {
		response.ErrorFrom(c, err)
		return nil, false
	}
	return account, true
}
