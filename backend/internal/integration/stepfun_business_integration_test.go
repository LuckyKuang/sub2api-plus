//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Every cell sends through the production handler/auth/scheduler, verified TLS
// and two saved authenticated CONNECT proxies. PostgreSQL/Redis assertions in
// runSevenBusinessFlow cover exact charges, deduplication, released leases and
// B affinity after A recovers and the gateway instance changes. The expected
// regions and paths below are independent literals from the Step-Code profile
// contract, and are never computed by production endpoint-selection helpers.
func TestStepFunBusinessMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const child = "SUB2API_STEP_BUSINESS_TEST_CA"
	if os.Getenv(child) == "" {
		trust := newSevenNativeTrust(t)
		command := exec.Command(os.Args[0], "-test.run=^TestStepFunBusinessMatrix$", "-test.v", "-test.timeout=8m")
		command.Env = append(os.Environ(), child+"="+trust.certificate,
			"SUB2API_STEP_BUSINESS_TEST_KEY="+trust.key, "SSL_CERT_FILE="+trust.certificate)
		output, err := command.CombinedOutput()
		t.Log(string(output))
		require.NoError(t, err, "Step TLS subprocess must complete every profile and conversion flow")
		require.Contains(t, string(output), "--- PASS: TestStepFunBusinessMatrix")
		// The CA subprocess's verbose output is not a go-test JSON event.
		// Publish an independently required outer child for every business cell,
		// so deleting or skipping an inner row cannot hide behind parent success.
		for _, profile := range []string{"oauth_plan_cn", "oauth_plan_global", "apikey_payg_cn", "apikey_payg_global", "apikey_plan_cn", "apikey_plan_global"} {
			for _, protocol := range []string{"chat", "responses", "messages"} {
				for _, stream := range []bool{false, true} {
					for _, failure := range []int{503, 429} {
						name := fmt.Sprintf("%s/%s/stream=%t/failure=%d", profile, protocol, stream, failure)
						t.Run(name, func(t *testing.T) {
							require.Equal(t, 1, strings.Count(string(output), "--- PASS: TestStepFunBusinessMatrix/"+name+" ("), "exact business cell must execute once in the trusted-CA subprocess")
						})
					}
				}
			}
		}
		return
	}
	trust := sevenNativeTrust{certificate: os.Getenv(child), key: os.Getenv("SUB2API_STEP_BUSINESS_TEST_KEY")}
	services := newSevenBusinessServices(t)
	provider := sevenBusinessProvider{"stepfun", "stepfun", "", "stepcode (linux 6.8.0-31-generic; x64)", map[string]string{"X-Step-Client": "stepcode", "X-Stainless-Package-Version": "6.40.0"}}
	for _, profile := range []struct{ name, kind, region, mode, host, path string }{
		{"oauth_plan_cn", "oauth", "cn", "coding", "api.stepfun.com", "/step_plan/v1/chat/completions"},
		{"oauth_plan_global", "oauth", "global", "coding", "api.stepfun.ai", "/step_plan/v1/chat/completions"},
		{"apikey_payg_cn", "apikey", "cn", "payg", "api.stepfun.com", "/v1/chat/completions"},
		{"apikey_payg_global", "apikey", "global", "payg", "api.stepfun.ai", "/v1/chat/completions"},
		{"apikey_plan_cn", "apikey", "cn", "coding", "api.stepfun.com", "/step_plan/v1/chat/completions"},
		{"apikey_plan_global", "apikey", "global", "coding", "api.stepfun.ai", "/step_plan/v1/chat/completions"},
	} {
		for _, ingress := range []struct{ name, path string }{{"chat", "/v1/chat/completions"}, {"responses", "/v1/responses"}, {"messages", "/v1/messages"}} {
			for _, stream := range []bool{false, true} {
				for _, failure := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
					t.Run(fmt.Sprintf("%s/%s/stream=%t/failure=%d", profile.name, ingress.name, stream, failure), func(t *testing.T) {
						native := sevenNativeScenario{trust: trust, protocol: "chat_completions", ingress: ingress.path,
							host: profile.host, path: profile.path, model: "step-3.5-flash", authorization: "Authorization",
							accountKind: profile.kind, region: profile.region, mode: profile.mode, nonStreaming: !stream}
						runSevenBusinessFlow(t, services, provider, failure, &native)
					})
				}
			}
		}
	}
}
