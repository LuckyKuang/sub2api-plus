package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"strings"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	_ "golang.org/x/image/webp"
)

const (
	SettingKeyConsoleAppearance = "console_appearance"
	settingKeyConsoleBackground = "console_background_image"
	consoleBackgroundPrefix     = "/api/v1/settings/console-background/"
	MaxConsoleBackgroundBytes   = 2 << 20
	defaultConsoleAccentColor   = "#14b8a6"
)

// ConsoleAppearance is public metadata only. Image bytes are stored separately.
// A nil transparency lets clients use the default opacity.
type ConsoleAppearance struct {
	BackgroundURL    string `json:"background_url"`
	LightMask        int    `json:"light_mask"`
	DarkMask         int    `json:"dark_mask"`
	CardTransparency *int   `json:"card_transparency"`
	AccentColor      string `json:"accent_color"`
}

type ConsoleAppearanceUpdate struct {
	// Omitted/null keeps the image; an empty string removes it.
	BackgroundImage  *string `json:"background_image"`
	LightMask        int     `json:"light_mask"`
	DarkMask         int     `json:"dark_mask"`
	CardTransparency *int    `json:"card_transparency"`
	// Omitted/null preserves the color; an empty string restores the default.
	AccentColor *string `json:"accent_color"`
}

func defaultConsoleAppearance() ConsoleAppearance {
	return ConsoleAppearance{LightMask: 30, DarkMask: 48, AccentColor: defaultConsoleAccentColor}
}

func validConsolePercentage(value int) bool { return value >= 0 && value <= 100 }

func normalizeConsoleAccentColor(value string) (string, bool) {
	if value == "" {
		return defaultConsoleAccentColor, true
	}
	if len(value) != 7 || value[0] != '#' {
		return "", false
	}
	if _, err := hex.DecodeString(value[1:]); err != nil {
		return "", false
	}
	return strings.ToLower(value), true
}

func validConsoleBackgroundURL(value string) bool {
	if value == "" {
		return true
	}
	if !strings.HasPrefix(value, consoleBackgroundPrefix) {
		return false
	}
	revision := strings.TrimPrefix(value, consoleBackgroundPrefix)
	decoded, err := hex.DecodeString(revision)
	return err == nil && len(decoded) == sha256.Size && revision == strings.ToLower(revision)
}

func parseConsoleAppearance(raw string) ConsoleAppearance {
	appearance := defaultConsoleAppearance()
	if raw == "" {
		return appearance
	}
	// Decode the color separately so a corrupt new field cannot discard an
	// otherwise valid background or the existing mask and opacity settings.
	stored := struct {
		*ConsoleAppearance
		AccentColor json.RawMessage `json:"accent_color"`
	}{ConsoleAppearance: &appearance}
	if json.Unmarshal([]byte(raw), &stored) != nil ||
		!validConsolePercentage(appearance.LightMask) || !validConsolePercentage(appearance.DarkMask) ||
		(appearance.CardTransparency != nil && !validConsolePercentage(*appearance.CardTransparency)) ||
		!validConsoleBackgroundURL(appearance.BackgroundURL) {
		return defaultConsoleAppearance()
	}
	var accent string
	if json.Unmarshal(stored.AccentColor, &accent) == nil {
		if normalized, ok := normalizeConsoleAccentColor(accent); ok {
			appearance.AccentColor = normalized
		}
	}
	return appearance
}

func (s *SettingService) GetConsoleAppearance(ctx context.Context) (ConsoleAppearance, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyConsoleAppearance})
	if err != nil {
		return ConsoleAppearance{}, err
	}
	return parseConsoleAppearance(values[SettingKeyConsoleAppearance]), nil
}

func invalidConsoleAppearance(message string) error {
	return infraerrors.New(http.StatusBadRequest, "INVALID_CONSOLE_APPEARANCE", message)
}

// normalizeConsoleBackground strips metadata and animation by decoding and
// re-encoding a bounded raster. Never persist a URL, SVG, or unchecked bytes.
func normalizeConsoleBackground(dataURL string) ([]byte, error) {
	if len(dataURL) > base64.StdEncoding.EncodedLen(MaxConsoleBackgroundBytes)+64 {
		return nil, invalidConsoleAppearance("Background image exceeds 2 MiB after compression")
	}
	header, encoded, ok := strings.Cut(dataURL, ",")
	if !ok || (header != "data:image/jpeg;base64" && header != "data:image/png;base64" && header != "data:image/webp;base64") {
		return nil, invalidConsoleAppearance("Background must be a JPEG, PNG, or WebP image")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > MaxConsoleBackgroundBytes {
		return nil, invalidConsoleAppearance("Invalid or oversized background image")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || cfg.Width*cfg.Height > 16_000_000 ||
		header != "data:image/"+format+";base64" {
		return nil, invalidConsoleAppearance("Invalid background image format or dimensions")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, invalidConsoleAppearance("Unable to decode background image")
	}
	flattened := image.NewRGBA(decoded.Bounds())
	draw.Draw(flattened, flattened.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(flattened, flattened.Bounds(), decoded, decoded.Bounds().Min, draw.Over)
	var output bytes.Buffer
	if err := jpeg.Encode(&output, flattened, &jpeg.Options{Quality: 85}); err != nil {
		return nil, invalidConsoleAppearance("Unable to encode background image")
	}
	if output.Len() > MaxConsoleBackgroundBytes {
		return nil, invalidConsoleAppearance("Background image exceeds 2 MiB after compression")
	}
	return output.Bytes(), nil
}

func (s *SettingService) UpdateConsoleAppearance(ctx context.Context, update ConsoleAppearanceUpdate) (ConsoleAppearance, error) {
	if !validConsolePercentage(update.LightMask) || !validConsolePercentage(update.DarkMask) ||
		(update.CardTransparency != nil && !validConsolePercentage(*update.CardTransparency)) {
		return ConsoleAppearance{}, invalidConsoleAppearance("Mask strength and transparency must be between 0 and 100")
	}
	var accentColor string
	if update.AccentColor != nil {
		var valid bool
		accentColor, valid = normalizeConsoleAccentColor(*update.AccentColor)
		if !valid {
			return ConsoleAppearance{}, invalidConsoleAppearance("Accent color must use the #RRGGBB format")
		}
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyConsoleAppearance, settingKeyConsoleBackground})
	if err != nil {
		return ConsoleAppearance{}, err
	}
	appearance := parseConsoleAppearance(values[SettingKeyConsoleAppearance])
	// Re-save the matching image snapshot with its metadata. Concurrent edits
	// remain last-writer-wins without leaving a URL paired with another image.
	updates := map[string]string{settingKeyConsoleBackground: values[settingKeyConsoleBackground]}
	if update.BackgroundImage != nil {
		appearance.BackgroundURL = ""
		updates[settingKeyConsoleBackground] = ""
		if *update.BackgroundImage != "" {
			data, err := normalizeConsoleBackground(*update.BackgroundImage)
			if err != nil {
				return ConsoleAppearance{}, err
			}
			sum := sha256.Sum256(data)
			appearance.BackgroundURL = consoleBackgroundPrefix + hex.EncodeToString(sum[:])
			updates[settingKeyConsoleBackground] = base64.StdEncoding.EncodeToString(data)
		}
	}
	appearance.LightMask = update.LightMask
	appearance.DarkMask = update.DarkMask
	appearance.CardTransparency = update.CardTransparency
	if update.AccentColor != nil {
		appearance.AccentColor = accentColor
	}
	encoded, err := json.Marshal(appearance)
	if err != nil {
		return ConsoleAppearance{}, err
	}
	updates[SettingKeyConsoleAppearance] = string(encoded)
	if err := s.settingRepo.SetMultiple(ctx, updates); err != nil {
		return ConsoleAppearance{}, err
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return appearance, nil
}

func (s *SettingService) GetConsoleBackground(ctx context.Context, revision string) ([]byte, error) {
	notFound := infraerrors.New(http.StatusNotFound, "CONSOLE_BACKGROUND_NOT_FOUND", "Background image not found")
	if !validConsoleBackgroundURL(consoleBackgroundPrefix + revision) {
		return nil, notFound
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyConsoleAppearance, settingKeyConsoleBackground})
	if err != nil {
		return nil, err
	}
	appearance := parseConsoleAppearance(values[SettingKeyConsoleAppearance])
	if appearance.BackgroundURL != consoleBackgroundPrefix+revision {
		return nil, notFound
	}
	encoded := values[settingKeyConsoleBackground]
	if len(encoded) > base64.StdEncoding.EncodedLen(MaxConsoleBackgroundBytes) {
		return nil, notFound
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return nil, notFound
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != revision {
		return nil, notFound
	}
	return data, nil
}
