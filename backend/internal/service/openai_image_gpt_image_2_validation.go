package service

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	gptImage2MinPixels       = 655_360
	gptImage2StableMaxPixels = 2_560 * 1_440
	gptImage2MaxPixels       = 8_294_400
	gptImage2MaxEdge         = 3_840
	gptImage2MaxAspectRatio  = 3
)

// ValidateGPTImage2Request applies the dimensions currently accepted by
// gpt-image-2. It runs for both synchronous and asynchronous OpenAI Images
// requests so direct API callers cannot bypass the UI validation.
func ValidateGPTImage2Request(req *OpenAIImagesRequest) error {
	if req == nil || strings.TrimSpace(req.Model) != "gpt-image-2" {
		return nil
	}
	quality := strings.ToLower(strings.TrimSpace(req.Quality))
	if quality != "" && quality != "auto" && quality != "low" && quality != "medium" && quality != "high" {
		return fmt.Errorf("quality must be one of auto, low, medium, high for gpt-image-2")
	}

	size := strings.ToLower(strings.TrimSpace(req.Size))
	if size == "" || size == "auto" {
		return nil
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return fmt.Errorf("size must be auto or WIDTHxHEIGHT for gpt-image-2")
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return fmt.Errorf("size must contain positive integer width and height for gpt-image-2")
	}
	if width%16 != 0 || height%16 != 0 {
		return fmt.Errorf("width and height must be multiples of 16 for gpt-image-2")
	}
	long, short := width, height
	if height > width {
		long, short = height, width
	}
	if long > gptImage2MaxEdge {
		return fmt.Errorf("longest image edge must not exceed %d pixels for gpt-image-2", gptImage2MaxEdge)
	}
	if long > short*gptImage2MaxAspectRatio {
		return fmt.Errorf("image aspect ratio must not exceed %d:1 for gpt-image-2", gptImage2MaxAspectRatio)
	}
	pixels := int64(width) * int64(height)
	if pixels < gptImage2MinPixels || pixels > gptImage2MaxPixels {
		return fmt.Errorf("image pixel count must be between %d and %d for gpt-image-2", gptImage2MinPixels, gptImage2MaxPixels)
	}
	return nil
}

// GPTImage2SizeIsExperimental indicates the documented experimental range.
// The value is exposed for clients/tests; it does not reject otherwise valid
// sizes because 4K requests are supported by the API.
func GPTImage2SizeIsExperimental(size string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(size)), "x")
	if len(parts) != 2 {
		return false
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	return widthErr == nil && heightErr == nil && width > 0 && height > 0 && int64(width)*int64(height) > gptImage2StableMaxPixels
}
