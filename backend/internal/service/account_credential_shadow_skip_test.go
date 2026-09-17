//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// shadowSkipTestRepo 是满足 AccountRepository 接口的最小 stub（只实现 GetByID）。
// 其他方法通过嵌入 nil 接口值满足编译，若被误调则 panic，便于发现意外调用路径。
type shadowSkipTestRepo struct {
	AccountRepository
	account *Account
}

func (r *shadowSkipTestRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return r.account, nil
}

func newShadowTestGinCtx() *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/200/test", nil)
	return c
}

// --- 1. CanRefresh 守卫 ---

// TestOpenAITokenRefresherSkipsShadow 验证影子账号不被后台 token 刷新器处理。
func TestOpenAITokenRefresherSkipsShadow(t *testing.T) {
	pid := int64(100)
	r := NewOpenAITokenRefresher(nil, nil)
	// 影子账号：ParentAccountID 非 nil → CanRefresh 应返回 false
	require.False(t, r.CanRefresh(&Account{ID: 200, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &pid}))
	// 普通账号：有 refresh_token → CanRefresh 应返回 true
	require.True(t, r.CanRefresh(&Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"refresh_token": "RT"}}))
}

// --- 2. TestAccountConnection 影子凭据解析 ---

// TestAccountTestServiceSkipsShadow 验证影子账号连接测试不再早拒,而是尝试解析母账号凭据。
func TestAccountTestServiceSkipsShadow(t *testing.T) {
	pid := int64(100)
	shadow := &Account{
		ID:              200,
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &pid,
	}
	repo := &shadowSkipTestRepo{account: shadow}
	svc := &AccountTestService{accountRepo: repo}
	c := newShadowTestGinCtx()

	err := svc.TestAccountConnection(c, 200, "", "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve spark shadow parent")
}
