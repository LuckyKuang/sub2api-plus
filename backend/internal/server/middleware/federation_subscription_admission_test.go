//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// serveSubscriptionKey runs one request through APIKeyAuth for a user whose key
// is bound to an unlimited subscription group with an active subscription.
func serveSubscriptionKey(t *testing.T, user service.User, admission bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	now := time.Now().UTC()
	group := &service.Group{
		ID:               2,
		Name:             "cc",
		Status:           service.StatusActive,
		Platform:         service.PlatformAnthropic,
		Hydrated:         true,
		SubscriptionType: service.SubscriptionTypeSubscription,
	}
	user.Role = service.RoleUser
	user.Status = service.StatusActive
	user.Concurrency = 3
	apiKey := &service.APIKey{
		ID: 13, UserID: user.ID, Key: "federated-subscription-key", Status: service.StatusActive,
		GroupID: &group.ID, User: &user, Group: group,
	}
	apiKeyRepo := &stubApiKeyRepo{getByKey: func(_ context.Context, key string) (*service.APIKey, error) {
		if key != apiKey.Key {
			return nil, service.ErrAPIKeyNotFound
		}
		keyCopy := *apiKey
		userCopy := user
		keyCopy.User = &userCopy
		return &keyCopy, nil
	}}
	subscription := &service.UserSubscription{
		ID: 4, UserID: user.ID, GroupID: group.ID, Status: service.SubscriptionStatusActive,
		ExpiresAt:        now.Add(24 * time.Hour),
		DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now,
	}
	subscriptionRepo := &stubUserSubscriptionRepo{
		getActive: func(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
			if userID != subscription.UserID || groupID != subscription.GroupID {
				return nil, service.ErrSubscriptionNotFound
			}
			copy := *subscription
			return &copy, nil
		},
		updateStatus: func(context.Context, int64, string) error { return nil },
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Federation.AdmissionCheckEnabled = admission
	apiKeyService := service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, cfg)
	subscriptionService := service.NewSubscriptionService(nil, subscriptionRepo, nil, nil, cfg)
	router := newAuthTestRouter(apiKeyService, subscriptionService, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(w, req)
	return w
}

// A federated user's subscription usage is still billed from the mainland
// balance, so an exhausted balance must reject even inside an unlimited
// subscription group.
func TestAPIKeyAuthFederatedSubscriptionRejectsExhaustedBalance(t *testing.T) {
	withFederationHook(t, nil)
	w := serveSubscriptionKey(t, service.User{ID: 16, Balance: 0, FederationUsageWatermarkSeq: 49168}, true)
	require.Equal(t, http.StatusForbidden, w.Code)
	requireAPIKeyAuthError(t, w, "INSUFFICIENT_BALANCE", "Insufficient account balance")
}

func TestAPIKeyAuthFederatedSubscriptionUsesWatermarkBalance(t *testing.T) {
	withFederationHook(t, func(ctx context.Context, userID int64, balance float64, watermarkSeq int64) (float64, error) {
		require.Equal(t, int64(16), userID)
		require.Equal(t, int64(49168), watermarkSeq)
		return -0.5, nil // unacked local usage already exceeds the mirrored balance
	})
	w := serveSubscriptionKey(t, service.User{ID: 16, Balance: 3, FederationUsageWatermarkSeq: 49168}, true)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestAPIKeyAuthFederatedSubscriptionAllowsPositiveBalance(t *testing.T) {
	withFederationHook(t, nil)
	w := serveSubscriptionKey(t, service.User{ID: 15, Balance: 459, FederationUsageWatermarkSeq: 49386}, true)
	require.Equal(t, http.StatusOK, w.Code)
}

// Local (never federated) subscriptions keep their original semantics.
func TestAPIKeyAuthLocalSubscriptionIgnoresBalance(t *testing.T) {
	withFederationHook(t, func(context.Context, int64, float64, int64) (float64, error) {
		t.Fatal("local subscription users must not consult the federation hook")
		return 0, nil
	})
	w := serveSubscriptionKey(t, service.User{ID: 2, Balance: 0}, true)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestAPIKeyAuthSubscriptionIgnoresBalanceWhenAdmissionDisabled(t *testing.T) {
	withFederationHook(t, nil)
	w := serveSubscriptionKey(t, service.User{ID: 16, Balance: 0, FederationUsageWatermarkSeq: 49168}, false)
	require.Equal(t, http.StatusOK, w.Code)
}
