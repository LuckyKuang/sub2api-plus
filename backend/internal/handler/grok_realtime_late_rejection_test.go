//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func (r *grokCredentialHandlerRepo) ApplyGrokRequestOutcomeIfUnchanged(_ context.Context, expected *service.Account, outcome service.GrokRequestOutcome) (service.GrokRequestOutcomeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.accounts {
		current := &r.accounts[index]
		if expected == nil || current.ID != expected.ID || current.Platform != expected.Platform || current.Type != expected.Type ||
			current.Status != expected.Status || current.Schedulable != expected.Schedulable || current.AutoPauseOnExpired != expected.AutoPauseOnExpired ||
			!reflect.DeepEqual(current.ProxyID, expected.ProxyID) || !reflect.DeepEqual(current.Proxy, expected.Proxy) ||
			!reflect.DeepEqual(current.TempUnschedulableUntil, expected.TempUnschedulableUntil) || current.TempUnschedulableReason != expected.TempUnschedulableReason ||
			!reflect.DeepEqual(current.RateLimitedAt, expected.RateLimitedAt) || !reflect.DeepEqual(current.RateLimitResetAt, expected.RateLimitResetAt) ||
			!reflect.DeepEqual(current.OverloadUntil, expected.OverloadUntil) || !reflect.DeepEqual(current.ExpiresAt, expected.ExpiresAt) {
			continue
		}
		currentJSON, currentErr := json.Marshal(current.Credentials)
		expectedJSON, expectedErr := json.Marshal(expected.Credentials)
		if currentErr != nil || expectedErr != nil || string(currentJSON) != string(expectedJSON) {
			continue
		}
		if len(outcome.Extra) > 0 || len(outcome.ModelRateLimits) > 0 {
			r.updateExtraIDs = append(r.updateExtraIDs, current.ID)
			if current.Extra == nil {
				current.Extra = map[string]any{}
			}
			for key, value := range outcome.Extra {
				current.Extra[key] = value
			}
		}
		if outcome.TempUnschedulableUntil != nil {
			r.setTempIDs = append(r.setTempIDs, current.ID)
			until := *outcome.TempUnschedulableUntil
			current.TempUnschedulableUntil, current.TempUnschedulableReason = &until, outcome.TempUnschedulableReason
		}
		if outcome.RateLimitResetAt != nil {
			r.rateLimitIDs = append(r.rateLimitIDs, current.ID)
			now, reset := time.Now(), *outcome.RateLimitResetAt
			current.RateLimitedAt, current.RateLimitResetAt = &now, &reset
		}
		if outcome.ClearRateLimit {
			current.RateLimitedAt, current.RateLimitResetAt = nil, nil
		}
		generation, _ := current.Extra["runtime_recovery_generation"].(string)
		return service.GrokRequestOutcomeResult{Applied: true, RecoveryGeneration: generation}, nil
	}
	return service.GrokRequestOutcomeResult{}, nil
}

// A separate process owns the test CA. The normal production WebSocket client
// still verifies the TLS origin, without changing process-wide trust in other
// handler tests or adding a production-only test transport hook.
func TestGrokRealtimeHandlerLateRejectionCannotCoolReplacementOwner(t *testing.T) {
	const childMarker = "SUB2API_GROK_LATE_REJECTION_CHILD"
	changes := []string{"unchanged", "relinked", "same-row-proxy-edited", "manual-pause", "other-cooldown", "proxy-authentication-required"}
	if os.Getenv(childMarker) != "1" {
		for _, change := range changes {
			t.Run(change, func(t *testing.T) {
				command := exec.Command(os.Args[0], "-test.run=^TestGrokRealtimeHandlerLateRejectionCannotCoolReplacementOwner$/^"+change+"$", "-test.v", "-test.count=1")
				command.Env = append(os.Environ(), childMarker+"=1")
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
				require.Contains(t, string(output), "--- PASS: TestGrokRealtimeHandlerLateRejectionCannotCoolReplacementOwner/"+change+" ")
				t.Log(string(output))
			})
		}
		return
	}
	gin.SetMode(gin.TestMode)
	var originRequests atomic.Int64
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		originRequests.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/v1/realtime" || request.URL.Query().Get("model") != "grok-voice-latest" || request.Header.Get("Authorization") != "Bearer handler-original-token" {
			t.Errorf("unexpected realtime handshake method/path/model/credential")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"isolated expired grant"}}`)
	}))
	t.Cleanup(origin.Close)
	certificateDir := t.TempDir()
	certificateFile := filepath.Join(certificateDir, "origin-ca.pem")
	require.NoError(t, os.WriteFile(certificateFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: origin.Certificate().Raw}), 0600))
	t.Setenv("SSL_CERT_FILE", certificateFile)
	t.Setenv("SSL_CERT_DIR", certificateDir)

	for _, change := range changes {
		t.Run(change, func(t *testing.T) {
			var proxyRequests atomic.Int64
			proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				proxyRequests.Add(1)
				if request.Method != http.MethodConnect || request.Host != origin.Listener.Addr().String() {
					http.Error(w, "isolated CONNECT target required", http.StatusBadRequest)
					return
				}
				if change == "proxy-authentication-required" {
					w.Header().Set("Proxy-Authenticate", `Basic realm="isolated-realtime"`)
					w.WriteHeader(http.StatusProxyAuthRequired)
					return
				}
				remote, err := net.DialTimeout("tcp", origin.Listener.Addr().String(), time.Second)
				if err != nil {
					http.Error(w, "isolated origin unavailable", http.StatusBadGateway)
					return
				}
				client, buffered, err := w.(http.Hijacker).Hijack()
				if err != nil {
					_ = remote.Close()
					return
				}
				defer func() { _ = client.Close(); _ = remote.Close() }()
				_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				if buffered.Flush() != nil {
					return
				}
				copied := make(chan struct{})
				go func() { _, _ = io.Copy(remote, buffered); close(copied) }()
				_, _ = io.Copy(client, remote)
				_ = remote.Close()
				_ = client.Close()
				<-copied
			}))
			t.Cleanup(proxyServer.Close)
			proxyURL, err := url.Parse(proxyServer.URL)
			require.NoError(t, err)
			proxyPort, err := strconv.Atoi(proxyURL.Port())
			require.NoError(t, err)
			account := grokRealtimePreflightAccount(&service.Proxy{ID: 7951, Protocol: "http", Host: proxyURL.Hostname(), Port: proxyPort,
				Username: "handler-proxy-user", Password: "handler-proxy-password", Status: service.StatusActive})
			account.Credentials["base_url"] = origin.URL + "/v1"
			cache := &grokRealtimeHandlerTokenCache{}
			h, repo, slots := grokRealtimePreflightHandler(t, account, cache)
			otherUntil := time.Now().Add(6 * time.Hour)
			cache.onRead = func() {
				repo.mu.Lock()
				defer repo.mu.Unlock()
				current := &repo.accounts[0]
				switch change {
				case "relinked":
					current.Credentials["access_token"] = "replacement-owner-token"
					current.Credentials["refresh_token"] = "replacement-owner-refresh"
				case "same-row-proxy-edited":
					current.Proxy.Password = "replacement-owner-proxy-password"
				case "manual-pause":
					current.Schedulable = false
				case "other-cooldown":
					current.TempUnschedulableUntil = &otherUntil
					current.TempUnschedulableReason = "independent-admin-cooldown"
				}
			}
			beforeRequests := originRequests.Load()
			c, recorder := grokRealtimePreflightContext()
			h.GrokRealtime(c)
			require.Equal(t, http.StatusBadGateway, recorder.Code)
			require.Equal(t, 1, cache.reads)
			require.Equal(t, int64(1), proxyRequests.Load())
			require.Empty(t, repo.setErrorIDs, "a handshake or proxy error must not invalidate the replacement grant")
			require.Zero(t, cache.deletes)
			require.Empty(t, repo.updateExtraIDs)
			slots.assertReleased(t)
			if change == "proxy-authentication-required" {
				require.Equal(t, beforeRequests, originRequests.Load(), "407 must not reach the TLS origin")
				return
			}
			require.Equal(t, beforeRequests+1, originRequests.Load(), "the real TLS origin must reject the captured grant with 401")
			repo.mu.Lock()
			current := repo.accounts[0]
			repo.mu.Unlock()
			if change == "unchanged" {
				require.Equal(t, []int64{account.ID}, repo.setTempIDs, "a current grant retains the normal unauthorized cooldown")
				require.NotNil(t, current.TempUnschedulableUntil)
				require.WithinDuration(t, time.Now().Add(10*time.Minute), *current.TempUnschedulableUntil, 3*time.Second)
				return
			}
			require.Empty(t, repo.setTempIDs, "a late rejection cannot overwrite another credential, route, or scheduling owner")
			if change == "other-cooldown" {
				require.Equal(t, &otherUntil, current.TempUnschedulableUntil)
				require.Equal(t, "independent-admin-cooldown", current.TempUnschedulableReason)
			} else {
				require.Nil(t, current.TempUnschedulableUntil)
			}
			if change == "manual-pause" {
				require.False(t, current.Schedulable)
			}
		})
	}
}
