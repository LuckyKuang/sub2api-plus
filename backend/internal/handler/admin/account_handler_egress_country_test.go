//go:build unit || !integration

package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountAdminBoundariesValidateEgressCountryExtra(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		body         string
		mount        func(*gin.Engine, *AccountHandler)
		setup        func(*stubAdminService)
		expectStatus int
		// expectBody 为空时断言响应 reason == "INVALID_EGRESS_COUNTRY"；
		// 批量接口返回逐项结果（HTTP 200），此时断言响应体包含该子串。
		expectBody string
	}{
		{
			name:         "create",
			method:       http.MethodPost,
			path:         "/accounts",
			body:         `{"name":"account","platform":"openai","type":"oauth","credentials":{},"extra":{"egress_country":"USA"}}`,
			mount:        func(router *gin.Engine, handler *AccountHandler) { router.POST("/accounts", handler.Create) },
			expectStatus: http.StatusBadRequest,
		},
		{
			name:         "update",
			method:       http.MethodPut,
			path:         "/accounts/1",
			body:         `{"extra":{"egress_country":"USA"}}`,
			mount:        func(router *gin.Engine, handler *AccountHandler) { router.PUT("/accounts/:id", handler.Update) },
			expectStatus: http.StatusBadRequest,
		},
		{
			name:         "bulk update",
			method:       http.MethodPost,
			path:         "/accounts/bulk-update",
			body:         `{"account_ids":[1],"extra":{"egress_country":"USA"}}`,
			mount: func(router *gin.Engine, handler *AccountHandler) {
				router.POST("/accounts/bulk-update", handler.BulkUpdate)
			},
			expectStatus: http.StatusBadRequest,
		},
		{
			name:   "batch create",
			method: http.MethodPost,
			path:   "/accounts/batch",
			body:   `{"accounts":[{"name":"account","platform":"openai","type":"oauth","credentials":{},"extra":{"egress_country":"USA"}}]}`,
			mount:  func(router *gin.Engine, handler *AccountHandler) { router.POST("/accounts/batch", handler.BatchCreate) },
			// 批量接口对逐项校验失败只记录结果，整体仍返回 200。
			expectStatus: http.StatusOK,
			expectBody:   "two-letter ISO 3166-1 alpha-2 country code",
		},
		{
			name:   "apply oauth credentials",
			method: http.MethodPost,
			path:   "/accounts/1/apply-oauth-credentials",
			body:   `{"type":"oauth","credentials":{},"extra":{"egress_country":"USA"}}`,
			mount: func(router *gin.Engine, handler *AccountHandler) {
				router.POST("/accounts/:id/apply-oauth-credentials", handler.ApplyOAuthCredentials)
			},
			setup: func(stub *stubAdminService) {
				stub.getAccountResult = &service.Account{
					ID:       1,
					Platform: service.PlatformOpenAI,
					Type:     service.AccountTypeOAuth,
				}
			},
			expectStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			stub := newStubAdminService()
			if tt.setup != nil {
				tt.setup(stub)
			}
			handler := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			tt.mount(router, handler)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			require.Equal(t, tt.expectStatus, recorder.Code)
			if tt.expectBody != "" {
				require.Contains(t, recorder.Body.String(), tt.expectBody)
				return
			}
			var responseBody struct {
				Reason string `json:"reason"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &responseBody))
			require.Equal(t, "INVALID_EGRESS_COUNTRY", responseBody.Reason)
		})
	}
}

func TestAccountCreateNormalizesLowercaseEgressCountry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := newStubAdminService()
	handler := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/accounts", handler.Create)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/accounts", bytes.NewBufferString(
		`{"name":"account","platform":"openai","type":"oauth","credentials":{},"extra":{"egress_country":" us "}}`,
	))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotEmpty(t, stub.createdAccounts)
	require.Equal(t, "US", stub.createdAccounts[0].Extra["egress_country"])
}
