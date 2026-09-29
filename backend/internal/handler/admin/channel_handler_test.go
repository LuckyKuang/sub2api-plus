//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func float64Ptr(v float64) *float64 { return &v }
func intPtr(v int) *int             { return &v }

// ---------------------------------------------------------------------------
// 1. channelToResponse
// ---------------------------------------------------------------------------

func TestChannelToResponse_NilInput(t *testing.T) {
	require.Nil(t, channelToResponse(nil))
}

func TestChannelToResponse_FullChannel(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	ch := &service.Channel{
		ID:                 42,
		Name:               "test-channel",
		Description:        "desc",
		Status:             "active",
		BillingModelSource: "upstream",
		RestrictModels:     true,
		CreatedAt:          now,
		UpdatedAt:          now.Add(time.Hour),
		GroupIDs:           []int64{1, 2, 3},
		ModelPricing: []service.ChannelModelPricing{
			{
				ID:              10,
				Platform:        "openai",
				Models:          []string{"gpt-4"},
				BillingMode:     service.BillingModeToken,
				InputPrice:      float64Ptr(0.01),
				OutputPrice:     float64Ptr(0.03),
				CacheWritePrice: float64Ptr(0.005),
				CacheReadPrice:  float64Ptr(0.002),
				PerRequestPrice: float64Ptr(0.5),
			},
		},
		ModelMapping: map[string]map[string]string{
			"anthropic": {"claude-3-haiku": "claude-haiku-3"},
		},
	}

	resp := channelToResponse(ch)
	require.NotNil(t, resp)
	require.Equal(t, int64(42), resp.ID)
	require.Equal(t, "test-channel", resp.Name)
	require.Equal(t, "desc", resp.Description)
	require.Equal(t, "active", resp.Status)
	require.Equal(t, "upstream", resp.BillingModelSource)
	require.True(t, resp.RestrictModels)
	require.Equal(t, []int64{1, 2, 3}, resp.GroupIDs)
	require.Equal(t, "2025-06-01T12:00:00Z", resp.CreatedAt)
	require.Equal(t, "2025-06-01T13:00:00Z", resp.UpdatedAt)

	// model mapping
	require.Len(t, resp.ModelMapping, 1)
	require.Equal(t, "claude-haiku-3", resp.ModelMapping["anthropic"]["claude-3-haiku"])

	// pricing
	require.Len(t, resp.ModelPricing, 1)
	p := resp.ModelPricing[0]
	require.Equal(t, int64(10), p.ID)
	require.Equal(t, "openai", p.Platform)
	require.Equal(t, []string{"gpt-4"}, p.Models)
	require.Equal(t, "token", p.BillingMode)
	require.Equal(t, float64Ptr(0.01), p.InputPrice)
	require.Equal(t, float64Ptr(0.03), p.OutputPrice)
	require.Equal(t, float64Ptr(0.005), p.CacheWritePrice)
	require.Equal(t, float64Ptr(0.002), p.CacheReadPrice)
	require.Equal(t, float64Ptr(0.5), p.PerRequestPrice)
	require.Empty(t, p.Intervals)
}

func TestChannelToResponse_EmptyDefaults(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ch := &service.Channel{
		ID:                 1,
		Name:               "ch",
		BillingModelSource: service.BillingModelSourceChannelMapped,
		CreatedAt:          now,
		UpdatedAt:          now,
		GroupIDs:           nil,
		ModelMapping:       nil,
		ModelPricing: []service.ChannelModelPricing{
			{
				Platform:    "",
				BillingMode: "",
				Models:      []string{"m1"},
			},
		},
	}

	// handler 层 channelToResponse 现在是纯透传：BillingModelSource 的空值兜底
	// 已下放到 service 层（Create/GetByID/List/Update/ListAvailable 出口统一处理），
	// 因此这里构造 fixture 时直接传入归一化后的值。
	resp := channelToResponse(ch)
	require.Equal(t, "channel_mapped", resp.BillingModelSource)
	require.NotNil(t, resp.GroupIDs)
	require.Empty(t, resp.GroupIDs)
	require.NotNil(t, resp.ModelMapping)
	require.Empty(t, resp.ModelMapping)

	require.Len(t, resp.ModelPricing, 1)
	require.Equal(t, "anthropic", resp.ModelPricing[0].Platform)
	require.Equal(t, "token", resp.ModelPricing[0].BillingMode)
}

func TestChannelToResponse_BillingModelSourcePassthrough(t *testing.T) {
	// handler 不再兜底 BillingModelSource：空值应原样透传（由 service 层负责默认回填）。
	ch := &service.Channel{
		ID:                 1,
		Name:               "ch",
		BillingModelSource: "",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	resp := channelToResponse(ch)
	require.Equal(t, "", resp.BillingModelSource, "handler 应纯透传，默认值由 service.normalizeBillingModelSource 负责")
}

func TestChannelToResponse_NilModels(t *testing.T) {
	now := time.Now()
	ch := &service.Channel{
		ID:        1,
		Name:      "ch",
		CreatedAt: now,
		UpdatedAt: now,
		ModelPricing: []service.ChannelModelPricing{
			{
				Models: nil,
			},
		},
	}

	resp := channelToResponse(ch)
	require.Len(t, resp.ModelPricing, 1)
	require.NotNil(t, resp.ModelPricing[0].Models)
	require.Empty(t, resp.ModelPricing[0].Models)
}

func TestChannelToResponse_WithIntervals(t *testing.T) {
	now := time.Now()
	ch := &service.Channel{
		ID:        1,
		Name:      "ch",
		CreatedAt: now,
		UpdatedAt: now,
		ModelPricing: []service.ChannelModelPricing{
			{
				Models:      []string{"m1"},
				BillingMode: service.BillingModePerRequest,
				Intervals: []service.PricingInterval{
					{
						ID:              100,
						MinTokens:       0,
						MaxTokens:       intPtr(1000),
						TierLabel:       "1K",
						InputPrice:      float64Ptr(0.01),
						OutputPrice:     float64Ptr(0.02),
						CacheWritePrice: float64Ptr(0.003),
						CacheReadPrice:  float64Ptr(0.001),
						PerRequestPrice: float64Ptr(0.1),
						SortOrder:       1,
					},
					{
						ID:        101,
						MinTokens: 1000,
						MaxTokens: nil,
						TierLabel: "unlimited",
						SortOrder: 2,
					},
				},
			},
		},
	}

	resp := channelToResponse(ch)
	require.Len(t, resp.ModelPricing, 1)
	intervals := resp.ModelPricing[0].Intervals
	require.Len(t, intervals, 2)

	iv0 := intervals[0]
	require.Equal(t, int64(100), iv0.ID)
	require.Equal(t, 0, iv0.MinTokens)
	require.Equal(t, intPtr(1000), iv0.MaxTokens)
	require.Equal(t, "1K", iv0.TierLabel)
	require.Equal(t, float64Ptr(0.01), iv0.InputPrice)
	require.Equal(t, float64Ptr(0.02), iv0.OutputPrice)
	require.Equal(t, float64Ptr(0.003), iv0.CacheWritePrice)
	require.Equal(t, float64Ptr(0.001), iv0.CacheReadPrice)
	require.Equal(t, float64Ptr(0.1), iv0.PerRequestPrice)
	require.Equal(t, 1, iv0.SortOrder)

	iv1 := intervals[1]
	require.Equal(t, int64(101), iv1.ID)
	require.Equal(t, 1000, iv1.MinTokens)
	require.Nil(t, iv1.MaxTokens)
	require.Equal(t, "unlimited", iv1.TierLabel)
	require.Equal(t, 2, iv1.SortOrder)
}

func TestChannelToResponse_MultipleEntries(t *testing.T) {
	now := time.Now()
	ch := &service.Channel{
		ID:        1,
		Name:      "multi",
		CreatedAt: now,
		UpdatedAt: now,
		ModelPricing: []service.ChannelModelPricing{
			{
				ID:          1,
				Platform:    "anthropic",
				Models:      []string{"claude-sonnet-4"},
				BillingMode: service.BillingModeToken,
				InputPrice:  float64Ptr(0.003),
				OutputPrice: float64Ptr(0.015),
			},
			{
				ID:              2,
				Platform:        "openai",
				Models:          []string{"gpt-4", "gpt-4o"},
				BillingMode:     service.BillingModePerRequest,
				PerRequestPrice: float64Ptr(1.0),
			},
			{
				ID:               3,
				Platform:         "gemini",
				Models:           []string{"gemini-2.5-pro"},
				BillingMode:      service.BillingModeImage,
				ImageOutputPrice: float64Ptr(0.05),
				PerRequestPrice:  float64Ptr(0.2),
			},
		},
	}

	resp := channelToResponse(ch)
	require.Len(t, resp.ModelPricing, 3)

	require.Equal(t, int64(1), resp.ModelPricing[0].ID)
	require.Equal(t, "anthropic", resp.ModelPricing[0].Platform)
	require.Equal(t, []string{"claude-sonnet-4"}, resp.ModelPricing[0].Models)
	require.Equal(t, "token", resp.ModelPricing[0].BillingMode)

	require.Equal(t, int64(2), resp.ModelPricing[1].ID)
	require.Equal(t, "openai", resp.ModelPricing[1].Platform)
	require.Equal(t, []string{"gpt-4", "gpt-4o"}, resp.ModelPricing[1].Models)
	require.Equal(t, "per_request", resp.ModelPricing[1].BillingMode)

	require.Equal(t, int64(3), resp.ModelPricing[2].ID)
	require.Equal(t, "gemini", resp.ModelPricing[2].Platform)
	require.Equal(t, []string{"gemini-2.5-pro"}, resp.ModelPricing[2].Models)
	require.Equal(t, "image", resp.ModelPricing[2].BillingMode)
	require.Equal(t, float64Ptr(0.05), resp.ModelPricing[2].ImageOutputPrice)
}

// ---------------------------------------------------------------------------
// 2. pricingRequestToService
// ---------------------------------------------------------------------------

func TestPricingRequestToService_Defaults(t *testing.T) {
	tests := []struct {
		name      string
		req       channelModelPricingRequest
		wantField string // which default field to check
		wantValue string
	}{
		{
			name: "empty billing mode defaults to token",
			req: channelModelPricingRequest{
				Models:      []string{"m1"},
				BillingMode: "",
			},
			wantField: "BillingMode",
			wantValue: string(service.BillingModeToken),
		},
		{
			name: "empty platform stays empty",
			req: channelModelPricingRequest{
				Models:   []string{"m1"},
				Platform: "",
			},
			wantField: "Platform",
			wantValue: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pricingRequestToService([]channelModelPricingRequest{tt.req}, true)
			require.Len(t, result, 1)
			switch tt.wantField {
			case "BillingMode":
				require.Equal(t, service.BillingMode(tt.wantValue), result[0].BillingMode)
			case "Platform":
				require.Equal(t, tt.wantValue, result[0].Platform)
			}
		})
	}
}

func TestPricingRequestToService_WithAllFields(t *testing.T) {
	reqs := []channelModelPricingRequest{
		{
			Platform:          "openai",
			Models:            []string{"gpt-4", "gpt-4o"},
			BillingMode:       "per_request",
			InputPrice:        float64Ptr(0.01),
			OutputPrice:       float64Ptr(0.03),
			CacheWritePrice:   float64Ptr(0.005),
			CacheWrite1hPrice: float64Ptr(0.008),
			CacheReadPrice:    float64Ptr(0.002),
			ImageOutputPrice:  float64Ptr(0.04),
			PerRequestPrice:   float64Ptr(0.5),
		},
	}

	result := pricingRequestToService(reqs, true)
	require.Len(t, result, 1)
	r := result[0]
	require.Equal(t, "openai", r.Platform)
	require.Equal(t, []string{"gpt-4", "gpt-4o"}, r.Models)
	require.Equal(t, service.BillingModePerRequest, r.BillingMode)
	require.Equal(t, float64Ptr(0.01), r.InputPrice)
	require.Equal(t, float64Ptr(0.03), r.OutputPrice)
	require.Equal(t, float64Ptr(0.005), r.CacheWritePrice)
	require.Equal(t, float64Ptr(0.008), r.CacheWrite1hPrice)
	require.Equal(t, float64Ptr(0.002), r.CacheReadPrice)
	require.Equal(t, float64Ptr(0.04), r.ImageOutputPrice)
	require.Equal(t, float64Ptr(0.5), r.PerRequestPrice)
}

func TestPricingRequestToService_WithIntervals(t *testing.T) {
	reqs := []channelModelPricingRequest{
		{
			Models:      []string{"m1"},
			BillingMode: "per_request",
			Intervals: []pricingIntervalRequest{
				{
					MinTokens:         0,
					MaxTokens:         intPtr(2000),
					TierLabel:         "small",
					InputPrice:        float64Ptr(0.01),
					OutputPrice:       float64Ptr(0.02),
					CacheWritePrice:   float64Ptr(0.003),
					CacheWrite1hPrice: float64Ptr(0.006),
					CacheReadPrice:    float64Ptr(0.001),
					PerRequestPrice:   float64Ptr(0.1),
					SortOrder:         1,
				},
				{
					MinTokens: 2000,
					MaxTokens: nil,
					TierLabel: "large",
					SortOrder: 2,
				},
			},
		},
	}

	result := pricingRequestToService(reqs, true)
	require.Len(t, result, 1)
	require.Len(t, result[0].Intervals, 2)

	iv0 := result[0].Intervals[0]
	require.Equal(t, 0, iv0.MinTokens)
	require.Equal(t, intPtr(2000), iv0.MaxTokens)
	require.Equal(t, "small", iv0.TierLabel)
	require.Equal(t, float64Ptr(0.01), iv0.InputPrice)
	require.Equal(t, float64Ptr(0.02), iv0.OutputPrice)
	require.Equal(t, float64Ptr(0.003), iv0.CacheWritePrice)
	require.Equal(t, float64Ptr(0.006), iv0.CacheWrite1hPrice)
	require.Equal(t, float64Ptr(0.001), iv0.CacheReadPrice)
	require.Equal(t, float64Ptr(0.1), iv0.PerRequestPrice)
	require.Equal(t, 1, iv0.SortOrder)

	iv1 := result[0].Intervals[1]
	require.Equal(t, 2000, iv1.MinTokens)
	require.Nil(t, iv1.MaxTokens)
	require.Equal(t, "large", iv1.TierLabel)
	require.Equal(t, 2, iv1.SortOrder)
}

func TestPricingRequestToService_EmptySlice(t *testing.T) {
	result := pricingRequestToService([]channelModelPricingRequest{}, true)
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestPricingRequestToService_NilPriceFields(t *testing.T) {
	reqs := []channelModelPricingRequest{
		{
			Models:      []string{"m1"},
			BillingMode: "token",
			// all price fields are nil by default
		},
	}

	result := pricingRequestToService(reqs, true)
	require.Len(t, result, 1)
	r := result[0]
	require.Nil(t, r.InputPrice)
	require.Nil(t, r.OutputPrice)
	require.Nil(t, r.CacheWritePrice)
	require.Nil(t, r.CacheReadPrice)
	require.Nil(t, r.ImageOutputPrice)
	require.Nil(t, r.PerRequestPrice)
}

func TestPricingRequestToService_TimePricing(t *testing.T) {
	req := channelModelPricingRequest{
		Models:      []string{"gpt-5"},
		BillingMode: "token",
		TimePricing: &channelTimePricingRequest{
			Timezone:     "Asia/Shanghai",
			WeekdaysOnly: true,
			Periods: []channelTimePricingPeriodRequest{{
				StartTime: "09:00", EndTime: "12:00", Multiplier: 2,
			}},
		},
	}

	got := pricingRequestToService([]channelModelPricingRequest{req}, true)
	require.Equal(t, "Asia/Shanghai", got[0].TimePricing.Timezone)
	require.True(t, got[0].TimePricing.WeekdaysOnly)
	require.Equal(t, 2.0, got[0].TimePricing.Periods[0].Multiplier)
}

func TestPricingRequestToService_TimePricingNil(t *testing.T) {
	got := pricingRequestToService([]channelModelPricingRequest{{Models: []string{"gpt-5"}}}, true)
	require.Nil(t, got[0].TimePricing)
}

// Fast/Flex 和区间倍率只用于渠道售价；思考等级倍率可在账号统计规则中独立配置。
func TestPricingRequestToService_MultipliersGatedByFlag(t *testing.T) {
	req := channelModelPricingRequest{
		Models:                     []string{"gpt-5"},
		BillingMode:                "token",
		FastMultiplier:             float64Ptr(2.5),
		FlexMultiplier:             float64Ptr(0.5),
		ReasoningEffortMultipliers: map[string]float64{"high": 1.5, "max": 3},
		Intervals: []pricingIntervalRequest{{
			MinTokens:            272000,
			InputMultiplier:      float64Ptr(2),
			OutputMultiplier:     float64Ptr(1.5),
			CacheWriteMultiplier: float64Ptr(2),
			CacheReadMultiplier:  float64Ptr(2),
		}},
	}

	allowed := pricingRequestToService([]channelModelPricingRequest{req}, true)
	require.Equal(t, float64Ptr(2.5), allowed[0].FastMultiplier)
	require.Equal(t, float64Ptr(0.5), allowed[0].FlexMultiplier)
	require.Equal(t, req.ReasoningEffortMultipliers, allowed[0].ReasoningEffortMultipliers)
	require.Equal(t, float64Ptr(2), allowed[0].Intervals[0].InputMultiplier)
	require.Equal(t, float64Ptr(1.5), allowed[0].Intervals[0].OutputMultiplier)
	require.Equal(t, float64Ptr(2), allowed[0].Intervals[0].CacheWriteMultiplier)
	require.Equal(t, float64Ptr(2), allowed[0].Intervals[0].CacheReadMultiplier)

	dropped := pricingRequestToService([]channelModelPricingRequest{req}, false)
	require.Nil(t, dropped[0].FastMultiplier)
	require.Nil(t, dropped[0].FlexMultiplier)
	require.Equal(t, req.ReasoningEffortMultipliers, dropped[0].ReasoningEffortMultipliers)
	require.Nil(t, dropped[0].Intervals[0].InputMultiplier)
	require.Nil(t, dropped[0].Intervals[0].OutputMultiplier)
	require.Nil(t, dropped[0].Intervals[0].CacheWriteMultiplier)
	require.Nil(t, dropped[0].Intervals[0].CacheReadMultiplier)
	// 非倍率字段不受开关影响
	require.Equal(t, 272000, dropped[0].Intervals[0].MinTokens)
}

func TestPricingToResponse_TimePricing(t *testing.T) {
	got := pricingToResponse(&service.ChannelModelPricing{
		BillingMode: service.BillingModeToken,
		TimePricing: &service.ChannelTimePricing{
			Timezone:     "Asia/Shanghai",
			WeekdaysOnly: true,
			Periods: []service.ChannelTimePricingPeriod{{
				StartTime: "14:00", EndTime: "18:00", Multiplier: 1.25,
			}},
		},
	})

	require.NotNil(t, got.TimePricing)
	require.Equal(t, "Asia/Shanghai", got.TimePricing.Timezone)
	require.True(t, got.TimePricing.WeekdaysOnly)
	require.Equal(t, 1.25, got.TimePricing.Periods[0].Multiplier)
}

func TestPricingToResponse_TimePricingNil(t *testing.T) {
	got := pricingToResponse(&service.ChannelModelPricing{})
	require.Nil(t, got.TimePricing)
}

func TestPricingRequestAndResponse_ReasoningEffortMultipliers(t *testing.T) {
	var req channelModelPricingRequest
	require.NoError(t, json.Unmarshal([]byte(`{"models":["custom-model"],"reasoning_effort_multipliers":{"none":0.5,"high":1.5,"max":3}}`), &req))
	pricing := pricingRequestToService([]channelModelPricingRequest{req}, true)
	got := pricingToResponse(&pricing[0])
	require.Equal(t, map[string]float64{"none": 0.5, "high": 1.5, "max": 3}, got.ReasoningEffortMultipliers)
	data, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(data), `"reasoning_effort_multipliers":{"high":1.5,"max":3,"none":0.5}`)
	require.NotContains(t, string(data), "max_reasoning_effort_multiplier")

	req = channelModelPricingRequest{}
	require.NoError(t, json.Unmarshal([]byte(`{"models":["custom-model"],"reasoning_effort_multipliers":{}}`), &req))
	pricing = pricingRequestToService([]channelModelPricingRequest{req}, true)
	require.Empty(t, pricing[0].ReasoningEffortMultipliers)
}

// ---------------------------------------------------------------------------
// 3. SyncPricingModels handler
// ---------------------------------------------------------------------------

func setupSyncPricingModelsRouter(pricingSvc *service.PricingService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &ChannelHandler{pricingService: pricingSvc, billingService: service.NewBillingService(nil, pricingSvc)}
	router.POST("/channels/pricing/sync-models", h.SyncPricingModels)
	router.GET("/channels/model-pricing", h.GetModelDefaultPricing)
	return router
}

// newCatalogBackedPricingService loads the bundled LiteLLM catalog so tests
// exercise the same model/pricing data the deployed service starts from.
func newCatalogBackedPricingService(t *testing.T) *service.PricingService {
	t.Helper()
	dataDir := t.TempDir()
	svc := service.NewPricingService(&config.Config{
		Pricing: config.PricingConfig{
			DataDir:      dataDir,
			FallbackFile: filepath.Join("..", "..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"),
		},
	}, nil)
	t.Cleanup(svc.Stop)
	require.NoError(t, svc.Initialize())
	return svc
}

// referencePayload 是同步/单模型查询返回的单个模型参考价载荷。
type referencePayload struct {
	Model        string                 `json:"model"`
	MatchedModel string                 `json:"matched_model"`
	Platform     string                 `json:"platform"`
	Status       string                 `json:"status"`
	Source       string                 `json:"source"`
	ReasonCode   string                 `json:"reason_code"`
	Pricing      map[string]interface{} `json:"pricing"`
}

func (r *referencePayload) price(key string) (*float64, bool) {
	if r == nil || r.Pricing == nil {
		return nil, false
	}
	raw, ok := r.Pricing[key]
	if !ok {
		return nil, false
	}
	value, ok := raw.(float64)
	if !ok {
		return nil, false
	}
	return &value, true
}

type syncPricingModelsPayload struct {
	Platform       string             `json:"platform"`
	RefreshStatus  string             `json:"refresh_status"`
	CatalogVersion string             `json:"catalog_version"`
	WarningCode    string             `json:"warning_code"`
	Models         []referencePayload `json:"models"`
}

func (p *syncPricingModelsPayload) reference(model string) *referencePayload {
	if p == nil {
		return nil
	}
	for i := range p.Models {
		if p.Models[i].Model == model {
			return &p.Models[i]
		}
	}
	return nil
}

func syncPricingModels(t *testing.T, router *gin.Engine, payload string) (*httptest.ResponseRecorder, *syncPricingModelsPayload) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/channels/pricing/sync-models", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var body struct {
		Data *syncPricingModelsPayload `json:"data"`
	}
	if w.Code != http.StatusOK {
		return w, nil
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return w, body.Data
}

// getModelPricing 走单模型查询，返回原始 recorder 供调用方断言。
func getModelPricing(t *testing.T, router *gin.Engine, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/channels/model-pricing"+query, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestSyncPricingModels_MissingPlatform(t *testing.T) {
	router := setupSyncPricingModelsRouter(service.NewPricingService(nil, nil))

	w, _ := syncPricingModels(t, router, `{}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSyncPricingModels_UnsupportedPlatform(t *testing.T) {
	router := setupSyncPricingModelsRouter(service.NewPricingService(nil, nil))

	w, _ := syncPricingModels(t, router, `{"platform":"unknown"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSyncPricingModels_ValidPlatform_EmptyService(t *testing.T) {
	router := setupSyncPricingModelsRouter(service.NewPricingService(nil, nil))

	for _, platform := range []string{
		"anthropic", "openai", "gemini", "antigravity", "grok",
		"kimi", "zhipu", "deepseek", "minimax", "opencode_go",
	} {
		w, data := syncPricingModels(t, router, `{"platform":"`+platform+`"}`)
		require.Equalf(t, http.StatusOK, w.Code, "platform=%s", platform)
		require.NotNilf(t, data, "platform=%s", platform)
		require.Equalf(t, platform, data.Platform, "platform=%s", platform)
		require.NotEmptyf(t, data.Models, "platform=%s must still list its supported models", platform)
		for _, ref := range data.Models {
			require.Equal(t, platform, ref.Platform)
			require.NotEmpty(t, ref.Status, "model=%s", ref.Model)
			switch ref.Status {
			case "priced":
				require.NotNilf(t, ref.Pricing, "model=%s must carry a price card", ref.Model)
				require.NotEmptyf(t, ref.Source, "model=%s", ref.Model)
			case "manual_required", "unsupported_unit":
				require.Nilf(t, ref.Pricing, "model=%s must not carry a zero price card", ref.Model)
				require.NotEmptyf(t, ref.ReasonCode, "model=%s", ref.Model)
			default:
				t.Fatalf("platform=%s model=%s unexpected status=%s", platform, ref.Model, ref.Status)
			}
		}
	}
}

// TestSyncPricingModels_ReturnsDefaultPricingPerModel 锁定回归：
// 「同步最新模型」必须连同官方默认价一起返回，否则前端只能建出没有价格的计价条目，
// 运营者得逐个模型手抄价（gpt-6-sol / gpt-6-luna / claude-opus-5-5 都踩过这个坑）。
func TestSyncPricingModels_ReturnsDefaultPricingPerModel(t *testing.T) {
	router := setupSyncPricingModelsRouter(newCatalogBackedPricingService(t))

	_, data := syncPricingModels(t, router, `{"platform":"openai"}`)
	require.NotNil(t, data)
	require.Equal(t, "openai", data.Platform)
	require.NotEmpty(t, data.CatalogVersion, "sync must report the catalog version")

	sol := data.reference("gpt-6-sol")
	require.NotNil(t, sol, "gpt-6-sol must be listed for platform=openai")
	luna := data.reference("gpt-6-luna")
	require.NotNil(t, luna, "gpt-6-luna must be listed for platform=openai")

	for _, ref := range []*referencePayload{sol, luna} {
		require.Equal(t, "priced", ref.Status, "model=%s", ref.Model)
		require.Equal(t, ref.Model, ref.MatchedModel, "model=%s", ref.Model)
		require.Equal(t, "openai", ref.Platform, "model=%s", ref.Model)
		require.Equal(t, "release_catalog", ref.Source, "model=%s", ref.Model)
		require.Empty(t, ref.ReasonCode, "model=%s", ref.Model)
		require.NotNil(t, ref.Pricing, "model=%s", ref.Model)
		require.Equal(t, "token", ref.Pricing["billing_mode"], "model=%s", ref.Model)
		require.Equal(t, []interface{}{ref.Model}, ref.Pricing["models"], "model=%s", ref.Model)
		for _, key := range []string{"input_price", "output_price", "cache_write_price", "cache_read_price"} {
			price, ok := ref.price(key)
			require.Truef(t, ok, "model=%s must carry %s", ref.Model, key)
			require.Positivef(t, *price, "model=%s %s", ref.Model, key)
		}
	}

	// Sol 与 Luna 价差 20 倍，串价会立刻在金额上暴露。
	for key, want := range map[string]float64{
		"input_price":       2e-6,
		"output_price":      10e-6,
		"cache_write_price": 2.5e-6,
		"cache_read_price":  0.2e-6,
	} {
		price, _ := sol.price(key)
		require.InDeltaf(t, want, *price, 1e-12, "gpt-6-sol %s", key)
	}
	for key, want := range map[string]float64{
		"input_price":       0.1e-6,
		"output_price":      0.5e-6,
		"cache_write_price": 0.125e-6,
		"cache_read_price":  0.01e-6,
	} {
		price, _ := luna.price(key)
		require.InDeltaf(t, want, *price, 1e-12, "gpt-6-luna %s", key)
	}
	fast, ok := sol.price("fast_multiplier")
	require.True(t, ok, "gpt-6-sol must report its Fast multiplier")
	require.InDelta(t, 2, *fast, 1e-12)

	// 272000 长上下文档位必须折算成区间，只给基础价不算完成。
	// 渠道区间左开右闭：min=272000 才能让 272000 走基础档、272001 进高档。
	raw, ok := sol.Pricing["intervals"].([]interface{})
	require.True(t, ok, "gpt-6-sol must expose long-context intervals")
	require.Len(t, raw, 1)
	interval := raw[0].(map[string]interface{})
	require.Equal(t, float64(272000), interval["min_tokens"])
	require.Equal(t, ">272000", interval["tier_label"])
	require.InDelta(t, 4e-6, interval["input_price"], 1e-12)
	require.InDelta(t, 15e-6, interval["output_price"], 1e-12)
	require.InDelta(t, 4e-7, interval["cache_read_price"], 1e-12)

	// Anthropic 平台同样要带回 claude-opus-5-5 的官方价。
	_, anthropic := syncPricingModels(t, router, `{"platform":"anthropic"}`)
	opus := anthropic.reference("claude-opus-5-5")
	require.NotNil(t, opus, "claude-opus-5-5 must be listed for platform=anthropic")
	require.Equal(t, "priced", opus.Status)
	for key, want := range map[string]float64{
		"input_price":          4e-6,
		"output_price":         20e-6,
		"cache_write_price":    5e-6,
		"cache_write_1h_price": 8e-6,
		"cache_read_price":     0.2e-6,
	} {
		price, ok := opus.price(key)
		require.Truef(t, ok, "claude-opus-5-5 must carry %s", key)
		require.InDeltaf(t, want, *price, 1e-12, "claude-opus-5-5 %s", key)
	}
}

// 目录里没有对应 provider 行的平台（Kimi/智谱/MiniMax/OpenCode Go）不得整平台
// 消失；没有精确价的型号必须保持可见且是 manual_required，而不是消失或全零假成功。
func TestSyncPricingModels_PlatformCoverageWithoutCatalogRows(t *testing.T) {
	router := setupSyncPricingModelsRouter(service.NewPricingService(nil, nil))

	_, data := syncPricingModels(t, router, `{"platform":"opencode_go"}`)
	require.NotNil(t, data)
	for _, model := range []string{
		"kimi-k2.7-code", "longcat-2.0", "mimo-v2.5", "mimo-v2.5-pro",
		"muse-spark-1.3-contributor", "muse-spark-1.2-contributor",
		"qwen3.8-max", "qwen3.8-flash", "qwen3.7-max", "qwen3.7-plus",
		"qwen3.6-plus", "hy4-preview", "hy3", "omen-alpha",
	} {
		ref := data.reference(model)
		require.NotNilf(t, ref, "opencode-go must keep unknown-price model %s visible", model)
		require.Equalf(t, "manual_required", ref.Status, "model=%s", model)
		require.Nilf(t, ref.Pricing, "model=%s must not carry a zero price card", model)
		require.Equalf(t, "exact_price_unavailable", ref.ReasonCode, "model=%s", model)
	}

	// 其它型号来自内置 fallback，必须是 priced 且带正价。
	_, data = syncPricingModels(t, router, `{"platform":"kimi"}`)
	kimi := data.reference("kimi-k2.6")
	require.NotNil(t, kimi)
	require.Equal(t, "priced", kimi.Status)
	require.Equal(t, "builtin_fallback", kimi.Source)
	price, ok := kimi.price("input_price")
	require.True(t, ok)
	require.InDelta(t, 0.95e-6, *price, 1e-12)
}

// TestSyncPricingModels_PricingMatchesSingleModelLookup 保证批量同步与单模型
// 查询共用同一份判定——否则两条路径会给出不同价格。
func TestSyncPricingModels_PricingMatchesSingleModelLookup(t *testing.T) {
	router := setupSyncPricingModelsRouter(newCatalogBackedPricingService(t))

	for _, platform := range []string{"openai", "anthropic"} {
		_, data := syncPricingModels(t, router, `{"platform":"`+platform+`"}`)
		require.NotNil(t, data)
		require.NotEmpty(t, data.Models)

		w := getModelPricing(t, router, "?platform="+platform+"&model="+data.Models[0].Model)
		require.Equal(t, http.StatusOK, w.Code)
		var envelope struct {
			Data referencePayload `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))

		require.Equal(t, data.Models[0].Status, envelope.Data.Status, "model=%s", data.Models[0].Model)
		require.Equal(t, data.Models[0].Source, envelope.Data.Source, "model=%s", data.Models[0].Model)
		require.Equal(t, data.Models[0].MatchedModel, envelope.Data.MatchedModel, "model=%s", data.Models[0].Model)
		require.Equal(t, data.Models[0].Pricing, envelope.Data.Pricing, "model=%s", data.Models[0].Model)
	}
}

// 平台与型号都是必填项：缺任一字段都是 400，而不是静默返回 found=false。
func TestGetModelDefaultPricing_RequiresPlatformAndModel(t *testing.T) {
	router := setupSyncPricingModelsRouter(newCatalogBackedPricingService(t))

	for _, query := range []string{
		"",
		"?model=gpt-6-sol",
		"?platform=openai",
		"?platform=unknown&model=gpt-6-sol",
	} {
		w := getModelPricing(t, router, query)
		require.Equalf(t, http.StatusBadRequest, w.Code, "query=%s", query)
	}
}

// 没有安全参考价不是错误：200 + manual_required + pricing=null + 稳定 reason code。
func TestGetModelDefaultPricing_ManualRequiredIsExplicit(t *testing.T) {
	router := setupSyncPricingModelsRouter(newCatalogBackedPricingService(t))
	for _, model := range []string{"kimi-k2.7-code", "omen-alpha", "no-such-model-anywhere"} {
		w := getModelPricing(t, router, "?platform=opencode_go&model="+model)
		require.Equalf(t, http.StatusOK, w.Code, "model=%s", model)

		var envelope struct {
			Data referencePayload `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		require.Equalf(t, "manual_required", envelope.Data.Status, "model=%s", model)
		require.Nilf(t, envelope.Data.Pricing, "model=%s must not carry a zero price card", model)
		require.Equalf(t, "none", envelope.Data.Source, "model=%s", model)
		require.Equalf(t, "exact_price_unavailable", envelope.Data.ReasonCode, "model=%s", model)
	}
}

// 跨平台不得串价：anthropic 的目录价不能返回给 openai 渠道，反之亦然。
func TestGetModelDefaultPricing_DoesNotCrossPlatforms(t *testing.T) {
	router := setupSyncPricingModelsRouter(newCatalogBackedPricingService(t))

	w := getModelPricing(t, router, "?platform=openai&model=claude-opus-5-5")
	require.Equal(t, http.StatusOK, w.Code)

	var envelope struct {
		Data referencePayload `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, "manual_required", envelope.Data.Status,
		"an Anthropic card must not be served to an OpenAI channel")
	require.Nil(t, envelope.Data.Pricing)

	w = getModelPricing(t, router, "?platform=anthropic&model=gpt-6-sol")
	require.Equal(t, http.StatusOK, w.Code)
	envelope.Data = referencePayload{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, "manual_required", envelope.Data.Status,
		"an OpenAI card must not be served to an Anthropic channel")
}

func TestGetModelDefaultPricing_ReturnsFable51CacheTTLs(t *testing.T) {
	router := setupSyncPricingModelsRouter(newCatalogBackedPricingService(t))
	w := getModelPricing(t, router, "?platform=anthropic&model=claude-fable-5-1")
	require.Equal(t, http.StatusOK, w.Code)

	var envelope struct {
		Data referencePayload `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, "priced", envelope.Data.Status)
	require.Equal(t, "claude-fable-5-1", envelope.Data.MatchedModel)
	require.Equal(t, "token", envelope.Data.Pricing["billing_mode"])
	cacheWrite, ok := envelope.Data.price("cache_write_price")
	require.True(t, ok)
	require.InDelta(t, 12.5e-6, *cacheWrite, 1e-12)
	cacheWrite1h, ok := envelope.Data.price("cache_write_1h_price")
	require.True(t, ok, "claude-fable-5-1 must carry its 1h cache-write tier")
	require.InDelta(t, 20e-6, *cacheWrite1h, 1e-12)
}

// 缺字段必须编码成 null，显式免费必须编码成 0——两者混同会让前端把「没有该
// 字段」显示成免费。kimi-k2.5 只有 input/output/cache-read 三档。
func TestGetModelDefaultPricing_PreservesFieldPresence(t *testing.T) {
	router := setupSyncPricingModelsRouter(newCatalogBackedPricingService(t))
	w := getModelPricing(t, router, "?platform=kimi&model=kimi-k2.5")
	require.Equal(t, http.StatusOK, w.Code)

	var envelope struct {
		Data referencePayload `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, "priced", envelope.Data.Status)
	require.Contains(t, envelope.Data.Pricing, "cache_write_1h_price")
	require.Nil(t, envelope.Data.Pricing["cache_write_1h_price"])
	require.Nil(t, envelope.Data.Pricing["image_input_price"])
	require.Nil(t, envelope.Data.Pricing["image_output_price"])
	require.Nil(t, envelope.Data.Pricing["per_request_price"])
	require.Empty(t, envelope.Data.Pricing["intervals"])
	price, ok := envelope.Data.price("input_price")
	require.True(t, ok)
	require.InDelta(t, 0.60e-6, *price, 1e-12)
}

// 供应商明确免费的条目（GLM-4.5-Flash / GLM-4.7-Flash）必须保留显式 0，
// 不能被当成「缺字段」补成 null。
func TestGetModelDefaultPricing_PreservesExplicitZero(t *testing.T) {
	router := setupSyncPricingModelsRouter(newCatalogBackedPricingService(t))
	w := getModelPricing(t, router, "?platform=zhipu&model=glm-4.5-flash")
	require.Equal(t, http.StatusOK, w.Code)

	var envelope struct {
		Data referencePayload `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, "priced", envelope.Data.Status)
	input, ok := envelope.Data.price("input_price")
	require.True(t, ok, "an explicit free field must still be present as 0")
	require.Zero(t, *input)
	output, ok := envelope.Data.price("output_price")
	require.True(t, ok, "an explicit free field must still be present as 0")
	require.Zero(t, *output)
}
