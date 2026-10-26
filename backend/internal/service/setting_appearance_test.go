//go:build unit

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// Requirements: docs/UI_INTERACTION.md Global console appearance.
type appearanceRepo struct {
	SettingRepository
	values map[string]string
	err    error
	writes int
	reads  []string
}

func (r *appearanceRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, key := range keys {
		r.reads = append(r.reads, key)
		out[key] = r.values[key]
	}
	return out, nil
}
func (r *appearanceRepo) SetMultiple(_ context.Context, values map[string]string) error {
	if r.err != nil {
		return r.err
	}
	for key, value := range values {
		r.values[key] = value
	}
	r.writes++
	return nil
}
func appearancePNG(t *testing.T, width int, value color.Color) string {
	t.Helper()
	bitmap := image.NewNRGBA(image.Rect(0, 0, width, 2))
	bitmap.Set(0, 0, value)
	var output bytes.Buffer
	require.NoError(t, png.Encode(&output, bitmap))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(output.Bytes())
}

func TestConsoleAppearancePublishReplaceAndRestore(t *testing.T) {
	ctx := context.Background()
	repo := &appearanceRepo{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	invalidations := 0
	svc.SetOnUpdateCallback(func() { invalidations++ })
	defaults, err := svc.GetConsoleAppearance(ctx)
	require.NoError(t, err)
	require.Equal(t, ConsoleAppearance{LightMask: 30, DarkMask: 48, AccentColor: "#14b8a6"}, defaults)
	imageURL := appearancePNG(t, 4, color.NRGBA{R: 255, A: 255})
	transparency := 45
	appearance, err := svc.UpdateConsoleAppearance(ctx, ConsoleAppearanceUpdate{BackgroundImage: &imageURL, LightMask: 30, DarkMask: 48, CardTransparency: &transparency})
	require.NoError(t, err)
	require.Equal(t, 1, invalidations)
	require.NotEmpty(t, appearance.BackgroundURL)
	stored, err := base64.StdEncoding.DecodeString(repo.values[settingKeyConsoleBackground])
	require.NoError(t, err)
	decoded, format, err := image.Decode(bytes.NewReader(stored))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, image.Rect(0, 0, 4, 2), decoded.Bounds())
	r, g, b, _ := decoded.At(3, 1).RGBA()
	require.Greater(t, r, uint32(55000))
	require.Greater(t, g, uint32(55000))
	require.Greater(t, b, uint32(55000))
	sum := sha256.Sum256(stored)
	require.Equal(t, "/api/v1/settings/console-background/"+hex.EncodeToString(sum[:]), appearance.BackgroundURL)
	activeRevision := strings.TrimPrefix(appearance.BackgroundURL, "/api/v1/settings/console-background/")
	data, err := svc.GetConsoleBackground(ctx, activeRevision)
	require.NoError(t, err)
	require.Equal(t, stored, data)
	repo.reads = nil
	public, err := svc.GetPublicSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, appearance, public.ConsoleAppearance)
	require.NotContains(t, repo.reads, settingKeyConsoleBackground, "public settings must never read image bytes")
	injected, err := svc.GetPublicSettingsForInjection(ctx)
	require.NoError(t, err)
	encoded, err := json.Marshal(injected)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "console_appearance")
	require.NotContains(t, string(encoded), repo.values[settingKeyConsoleBackground])
	// Adjustments omit image bytes and retain the existing active image.
	appearance, err = svc.UpdateConsoleAppearance(ctx, ConsoleAppearanceUpdate{LightMask: 0, DarkMask: 100, CardTransparency: &transparency})
	require.NoError(t, err)
	require.Equal(t, 0, appearance.LightMask)
	require.Equal(t, 100, appearance.DarkMask)
	data, err = svc.GetConsoleBackground(ctx, activeRevision)
	require.NoError(t, err)
	require.Equal(t, stored, data)
	second := appearancePNG(t, 4, color.NRGBA{B: 255, A: 255})
	replaced, err := svc.UpdateConsoleAppearance(ctx, ConsoleAppearanceUpdate{BackgroundImage: &second, LightMask: 30, DarkMask: 48})
	require.NoError(t, err)
	require.NotEqual(t, appearance.BackgroundURL, replaced.BackgroundURL)
	_, err = svc.GetConsoleBackground(ctx, activeRevision)
	require.Equal(t, 404, infraerrors.Code(err))
	empty := ""
	restored, err := svc.UpdateConsoleAppearance(ctx, ConsoleAppearanceUpdate{BackgroundImage: &empty, LightMask: 30, DarkMask: 48})
	require.NoError(t, err)
	require.Equal(t, defaults, restored)
	require.Empty(t, repo.values[settingKeyConsoleBackground])
	_, err = svc.GetConsoleBackground(ctx, strings.TrimPrefix(replaced.BackgroundURL, "/api/v1/settings/console-background/"))
	require.Equal(t, 404, infraerrors.Code(err))
}

func TestConsoleAppearanceRejectedUpdatesHaveNoSideEffects(t *testing.T) {
	valid := appearancePNG(t, 4, color.NRGBA{G: 255, A: 255})
	oversized := "data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(2<<20)+4)
	oversizedDimensions := appearancePNG(t, 4097, color.White)
	truncated, err := base64.StdEncoding.DecodeString(strings.Split(valid, ",")[1])
	require.NoError(t, err)
	truncatedURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(truncated[:40])
	negative, over := -1, 101
	cases := []ConsoleAppearanceUpdate{
		{LightMask: -1}, {LightMask: 101}, {DarkMask: -1}, {DarkMask: 101},
		{CardTransparency: &negative}, {CardTransparency: &over},
	}
	for _, input := range []string{"https://example.com/image.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/jpeg;base64,@@@", strings.Replace(valid, "image/png", "image/jpeg", 1), oversized, oversizedDimensions, truncatedURL} {
		value := input
		cases = append(cases, ConsoleAppearanceUpdate{BackgroundImage: &value})
	}
	for _, input := range []string{"#fff", "f59e0b", "#f59e0b00", "#zz0000", " #f59e0b", "#f59e0b ", "rgb(245,158,11)", "var(--accent)"} {
		value := input
		cases = append(cases, ConsoleAppearanceUpdate{AccentColor: &value, BackgroundImage: &valid})
	}
	for index, update := range cases {
		t.Run(string(rune('A'+index)), func(t *testing.T) {
			repo := &appearanceRepo{values: map[string]string{SettingKeyConsoleAppearance: `{"background_url":"","light_mask":30,"dark_mask":48,"card_transparency":45}`}}
			svc := NewSettingService(repo, &config.Config{})
			callback := false
			svc.SetOnUpdateCallback(func() { callback = true })
			before := repo.values[SettingKeyConsoleAppearance]
			_, err := svc.UpdateConsoleAppearance(context.Background(), update)
			require.Equal(t, 400, infraerrors.Code(err))
			require.Zero(t, repo.writes)
			require.Equal(t, before, repo.values[SettingKeyConsoleAppearance])
			require.False(t, callback)
		})
	}
	for _, amount := range []int{0, 100} {
		repo := &appearanceRepo{values: map[string]string{}}
		svc := NewSettingService(repo, &config.Config{})
		result, err := svc.UpdateConsoleAppearance(context.Background(), ConsoleAppearanceUpdate{CardTransparency: &amount})
		require.NoError(t, err)
		require.Equal(t, amount, *result.CardTransparency)
	}
	repo := &appearanceRepo{values: map[string]string{}, err: errors.New("storage failed")}
	svc := NewSettingService(repo, &config.Config{})
	callback := false
	svc.SetOnUpdateCallback(func() { callback = true })
	_, err = svc.UpdateConsoleAppearance(context.Background(), ConsoleAppearanceUpdate{BackgroundImage: &valid})
	require.Error(t, err)
	require.Empty(t, repo.values)
	require.False(t, callback)
}

func TestConsoleAppearanceCorruptStorageAndAuditPrivacy(t *testing.T) {
	for _, raw := range []string{"not json", `{"background_url":"https://example.com/a"}`, `{"light_mask":101}`, `{"card_transparency":-1}`} {
		repo := &appearanceRepo{values: map[string]string{SettingKeyConsoleAppearance: raw}}
		svc := NewSettingService(repo, &config.Config{})
		result, err := svc.GetConsoleAppearance(context.Background())
		require.NoError(t, err)
		require.Equal(t, ConsoleAppearance{LightMask: 30, DarkMask: 48, AccentColor: "#14b8a6"}, result)
	}
	body := RedactAuditBody([]byte(`{"background_image":"private-user-image","light_mask":30}`), "application/json")
	require.NotContains(t, body, "private-user-image")
	require.Contains(t, body, "light_mask")
}

// The approved global accent is persistent public metadata, defaults to the
// current green, and must not be lost when an older client omits the new field.
func TestConsoleAppearanceAccentPublishPreserveAndRestore(t *testing.T) {
	ctx := context.Background()
	repo := &appearanceRepo{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	invalidations := 0
	svc.SetOnUpdateCallback(func() { invalidations++ })
	color := "#F59E0B"
	transparency := 100
	result, err := svc.UpdateConsoleAppearance(ctx, ConsoleAppearanceUpdate{
		LightMask: 30, DarkMask: 48, CardTransparency: &transparency, AccentColor: &color,
	})
	require.NoError(t, err)
	require.Equal(t, "#f59e0b", result.AccentColor)
	require.Equal(t, 100, *result.CardTransparency)
	require.Equal(t, 1, repo.writes)
	require.Equal(t, 1, invalidations)
	require.Contains(t, repo.values[SettingKeyConsoleAppearance], `"accent_color":"#f59e0b"`)

	reloaded, err := svc.GetConsoleAppearance(ctx)
	require.NoError(t, err)
	require.Equal(t, result, reloaded)
	public, err := svc.GetPublicSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "#f59e0b", public.ConsoleAppearance.AccentColor)
	injected, err := svc.GetPublicSettingsForInjection(ctx)
	require.NoError(t, err)
	encoded, err := json.Marshal(injected)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"accent_color":"#f59e0b"`)

	result, err = svc.UpdateConsoleAppearance(ctx, ConsoleAppearanceUpdate{LightMask: 12, DarkMask: 58})
	require.NoError(t, err)
	require.Equal(t, "#f59e0b", result.AccentColor)
	reset := ""
	result, err = svc.UpdateConsoleAppearance(ctx, ConsoleAppearanceUpdate{LightMask: 12, DarkMask: 58, AccentColor: &reset})
	require.NoError(t, err)
	require.Equal(t, "#14b8a6", result.AccentColor)
	reloaded, err = svc.GetConsoleAppearance(ctx)
	require.NoError(t, err)
	require.Equal(t, "#14b8a6", reloaded.AccentColor)
}

// Existing installations keep all their valid appearance metadata when the
// optional accent is missing or corrupt. Only that field falls back to green.
func TestConsoleAppearanceAccentStorageFallbackPreservesBackground(t *testing.T) {
	path := "/api/v1/settings/console-background/" + strings.Repeat("a", 64)
	for _, test := range []struct {
		name   string
		field  string
		accent string
	}{
		{name: "legacy", accent: "#14b8a6"},
		{name: "null", field: `,"accent_color":null`, accent: "#14b8a6"},
		{name: "empty", field: `,"accent_color":""`, accent: "#14b8a6"},
		{name: "invalid color", field: `,"accent_color":"#fff"`, accent: "#14b8a6"},
		{name: "number", field: `,"accent_color":42`, accent: "#14b8a6"},
		{name: "object", field: `,"accent_color":{}`, accent: "#14b8a6"},
		{name: "uppercase", field: `,"accent_color":"#AABBCC"`, accent: "#aabbcc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := `{"background_url":"` + path + `","light_mask":18,"dark_mask":63,"card_transparency":72` + test.field + `}`
			repo := &appearanceRepo{values: map[string]string{SettingKeyConsoleAppearance: raw}}
			svc := NewSettingService(repo, &config.Config{})
			result, err := svc.GetConsoleAppearance(context.Background())
			require.NoError(t, err)
			require.Equal(t, path, result.BackgroundURL)
			require.Equal(t, 18, result.LightMask)
			require.Equal(t, 63, result.DarkMask)
			require.Equal(t, 72, *result.CardTransparency)
			require.Equal(t, test.accent, result.AccentColor)
			require.Zero(t, repo.writes)
		})
	}
}
