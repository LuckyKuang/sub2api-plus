//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Oracle: Codex f9e7b19df8, codex-api/src/images.rs and the imagegen extension
// integration fixtures: GIF options are siblings, omitted controls use API defaults.
func TestOfficialCodexGIFControlsReachImagesEndpoint(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		for _, endpoint := range []string{"generations", "edits"} {
			for _, controls := range []string{``, `,"frame_count":2,"frame_duration_ms":20`, `,"frame_count":16,"frame_duration_ms":2000`} {
				t.Run(kind+"/"+endpoint+controls, func(t *testing.T) {
					image := ""
					if endpoint == "edits" {
						image = `,"images":[{"image_url":"data:image/png;base64,AA=="}]`
					}
					body := []byte(`{"model":"gpt-image-2","prompt":"  animate this  ","output_format":"gif","response_format":"url"` + controls + image + `}`)
					c, rec := newOpenAIImagesTestContext(t, body)
					c.Request.URL.Path = "/v1/images/" + endpoint
					upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
					svc := newOpenAIImagesTestService(upstream)
					parsed, err := svc.ParseOpenAIImagesRequest(c, body)
					require.NoError(t, err)
					owner := directImagesTestAccount()
					owner.Type = kind
					if kind == AccountTypeAPIKey {
						owner = newOpenAIImagesAPIKeyAccount()
					}
					_, err = svc.ForwardImages(context.Background(), c, owner, body, parsed, "")
					require.NoError(t, err)
					require.Equal(t, "gif", gjson.GetBytes(upstream.lastBody, "output_format").String())
					require.Equal(t, "  animate this  ", gjson.GetBytes(upstream.lastBody, "prompt").String())
					require.False(t, gjson.GetBytes(upstream.lastBody, "gif").Exists(), "tool options must be flattened on the Images wire")
					require.Equal(t, controls != "", gjson.GetBytes(upstream.lastBody, "frame_count").Exists())
					require.Equal(t, controls != "", gjson.GetBytes(upstream.lastBody, "frame_duration_ms").Exists())
					if controls != "" {
						require.Equal(t, gjson.GetBytes(body, "frame_count").Int(), gjson.GetBytes(upstream.lastBody, "frame_count").Int())
						require.Equal(t, gjson.GetBytes(body, "frame_duration_ms").Int(), gjson.GetBytes(upstream.lastBody, "frame_duration_ms").Int())
					}
					require.Equal(t, "codex_cli_rs/0.167.0 (Ubuntu 24.04; x86_64) xterm-256color", upstream.lastReq.UserAgent())
					if kind != AccountTypeAPIKey {
						require.Equal(t, "/backend-api/codex/images/"+endpoint, upstream.lastReq.URL.Path)
						require.True(t, strings.HasPrefix(gjson.GetBytes(rec.Body.Bytes(), "data.0.url").String(), "data:image/gif;base64,"))
					}
				})
			}
		}
	}
}

func TestOfficialCodexGIFMultipartControlsAndValidation(t *testing.T) {
	for _, format := range []string{"json", "multipart"} {
		for _, tc := range []struct {
			field, value string
			valid        bool
		}{
			{"frame_count", "2", true}, {"frame_count", "16", true},
			{"frame_count", "1", false}, {"frame_count", "17", false}, {"frame_count", "2.5", false}, {"frame_count", `"2"`, false},
			{"frame_duration_ms", "20", true}, {"frame_duration_ms", "2000", true},
			{"frame_duration_ms", "19", false}, {"frame_duration_ms", "2001", false}, {"frame_duration_ms", "null", false},
		} {
			t.Run(format+"/"+tc.field+tc.value, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"gpt-image-2","prompt":"animate","output_format":"gif",%q:%s}`, tc.field, tc.value))
				contentType := "application/json"
				if format == "multipart" {
					var b bytes.Buffer
					w := multipart.NewWriter(&b)
					require.NoError(t, w.WriteField("model", "gpt-image-2"))
					require.NoError(t, w.WriteField("prompt", "animate"))
					require.NoError(t, w.WriteField("output_format", "gif"))
					require.NoError(t, w.WriteField(tc.field, tc.value))
					part, err := w.CreateFormFile("image", "input.png")
					require.NoError(t, err)
					_, err = part.Write([]byte("png"))
					require.NoError(t, err)
					require.NoError(t, w.Close())
					body, contentType = b.Bytes(), w.FormDataContentType()
				}
				c, _ := newOpenAIImagesTestContext(t, body)
				if format == "multipart" {
					c.Request.URL.Path = "/v1/images/edits"
				}
				c.Request.Header.Set("Content-Type", contentType)
				svc := newOpenAIImagesTestService(nil)
				parsed, err := svc.ParseOpenAIImagesRequest(c, body)
				if !tc.valid {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				payload, _, err := buildOpenAIImagesOAuthPayload(parsed, parsed.Model)
				require.NoError(t, err)
				require.Equal(t, tc.value, gjson.GetBytes(payload, tc.field).Raw)
			})
		}
	}
}

func TestOfficialCodexGIFNeverFallsBackAndDropsAnimation(t *testing.T) {
	for _, status := range []int{404, 405} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				require.Equal(t, "/backend-api/codex/images/generations", req.URL.Path)
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"endpoint unavailable"}}`))}, nil
			}}
			body := []byte(`{"model":"gpt-image-2","prompt":"animate","output_format":"gif","frame_count":2}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			svc := newOpenAIImagesTestService(upstream)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			_, err = svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
			require.Error(t, err)
			require.Equal(t, 1, calls, "an unavailable native endpoint cannot silently send an animation to Responses")
			_, err = buildOpenAIImagesResponsesRequest(parsed, "gpt-image-1")
			require.ErrorContains(t, err, "native Codex Images endpoint")
		})
	}
}
