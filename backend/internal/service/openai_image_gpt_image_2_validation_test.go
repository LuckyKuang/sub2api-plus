package service

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateGPTImage2RequestCustomResolutionAndQuality(t *testing.T) {
	tests := []struct {
		name    string
		size    string
		quality string
		wantErr string
	}{
		{name: "auto", size: "auto", quality: "auto"},
		{name: "valid HD", size: "1920x1088", quality: "medium"},
		{name: "valid QHD", size: "2560x1440", quality: "high"},
		{name: "valid 4K", size: "3840x2160", quality: "low"},
		{name: "not multiple of 16", size: "1920x1080", quality: "auto", wantErr: "multiples of 16"},
		{name: "edge too large", size: "4096x2160", quality: "auto", wantErr: "must not exceed"},
		{name: "too many pixels", size: "3840x3840", quality: "auto", wantErr: "pixel count"},
		{name: "ratio too wide", size: "3072x1008", quality: "auto", wantErr: "aspect ratio"},
		{name: "invalid quality", size: "1024x1024", quality: "ultra", wantErr: "quality must be one of"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateGPTImage2Request(&OpenAIImagesRequest{Model: "gpt-image-2", Size: test.size, Quality: test.quality})
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}

	require.True(t, GPTImage2SizeIsExperimental("3840x2160"))
	require.False(t, GPTImage2SizeIsExperimental("2560x1440"))
}

func TestParseOpenAIImagesRequestValidatesExactGPTImage2Only(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parse := func(model string) error {
		body := []byte(`{"model":"` + model + `","prompt":"draw a cat","size":"1920x1080"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = req
		_, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(ctx, body)
		return err
	}

	require.ErrorContains(t, parse("gpt-image-2"), "multiples of 16")
	require.NoError(t, parse("gpt-image-1"), "legacy image models keep their existing passthrough behavior")
}
