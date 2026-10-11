package service

import (
	"context"
	"net/url"
	"strings"
	"unicode"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

func (s *PaymentConfigService) SetOnUpdateCallback(callback func()) { s.onUpdate = callback }

func ValidateRedeemPurchase(enabled bool, raw, mode string) error {
	invalid := func() error {
		return infraerrors.BadRequest("INVALID_REDEEM_PURCHASE_CONFIG", "redeem purchase requires an absolute HTTP(S) URL without credentials and mode iframe or new_tab")
	}
	if mode != "iframe" && mode != "new_tab" {
		return invalid()
	}
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return invalid()
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if enabled {
			return invalid()
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.Contains(u.Host, "*") {
		return invalid()
	}
	return nil
}

// Validate before any system-settings write, so an invalid URL cannot partially
// save unrelated settings. Both configuration APIs use the same contract.
func (s *PaymentConfigService) ValidateRedeemPurchaseUpdate(ctx context.Context, req UpdatePaymentConfigRequest) error {
	if req.RedeemPurchaseEnabled == nil && req.RedeemPurchaseURL == nil && req.RedeemPurchaseMode == nil {
		return nil
	}
	vals, err := s.settingRepo.GetMultiple(ctx, []string{SettingPaymentRedeemPurchaseEnabled, SettingPaymentRedeemPurchaseURL, SettingPaymentRedeemPurchaseMode})
	if err != nil {
		return err
	}
	enabled, raw, mode := vals[SettingPaymentRedeemPurchaseEnabled] == "true", vals[SettingPaymentRedeemPurchaseURL], vals[SettingPaymentRedeemPurchaseMode]
	if mode == "" {
		mode = "new_tab"
	}
	if req.RedeemPurchaseEnabled != nil {
		enabled = *req.RedeemPurchaseEnabled
	}
	if req.RedeemPurchaseURL != nil {
		raw = *req.RedeemPurchaseURL
	}
	if req.RedeemPurchaseMode != nil {
		mode = *req.RedeemPurchaseMode
	}
	return ValidateRedeemPurchase(enabled, raw, mode)
}
