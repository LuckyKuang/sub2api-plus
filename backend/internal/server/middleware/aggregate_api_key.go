package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	pkghttputil "github.com/LuckyKuang/sub2api-plus/internal/pkg/httputil"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/requestmodel"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Called after credential, user and IP checks, before the existing group and
// billing checks. Only local capability/grant reads occur before canonical audit.
func prepareAggregateAPIKey(c *gin.Context, svc *service.APIKeyService, key *service.APIKey, google bool) (*service.APIKey, bool) {
	if !key.IsAggregate() {
		return key, true
	}
	fail := func(err error) (*service.APIKey, bool) {
		status, body := infraerrors.ToHTTP(err)
		if google {
			abortWithGoogleError(c, status, body.Message)
		} else {
			AbortWithError(c, status, body.Reason, body.Message)
		}
		return nil, false
	}
	scope, err := svc.AggregateScope(c.Request.Context(), key)
	if err != nil {
		return fail(err)
	}
	path := strings.TrimRight(c.Request.URL.Path, "/")
	discovery := c.Request.Method == http.MethodGet && (strings.HasSuffix(path, "/models") || strings.Contains(c.FullPath(), "/models/:model"))
	if discovery {
		protocol := ""
		if strings.Contains(path, "/v1beta/") {
			protocol = "gemini"
		}
		if c.Query("client_version") != "" {
			protocol = service.APIProtocolResponses
		}
		scope.AggregateModelIDs, err = svc.AggregateModels(c.Request.Context(), scope, protocol)
		if err != nil {
			return fail(err)
		}
		return scope, true
	}
	if path == "/v1/usage" || path == "/backend-api/wham/usage" {
		return scope, true
	}
	var hint int64
	if raw := c.GetHeader("X-Composite-Group-ID"); raw != "" {
		hint, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || hint <= 0 {
			return fail(infraerrors.BadRequest("AGGREGATE_GROUP_INVALID", "X-Composite-Group-ID must be a positive group ID"))
		}
	}
	model := c.Param("model")
	if action := strings.TrimPrefix(c.Param("modelAction"), "/"); action != "" {
		model = strings.Split(action, ":")[0]
	}
	stateful := c.Request.Method == http.MethodGet && !discovery || c.Request.Method == http.MethodDelete || c.Request.Method == http.MethodPatch
	filesUpload := c.Request.Method == http.MethodPost && (strings.HasSuffix(path, "/files") || strings.HasSuffix(path, "/files/upload"))
	if c.Request.Body != nil && c.Request.Method != http.MethodGet && !filesUpload {
		body, readErr := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if readErr != nil {
			if !RejectRequestBodyReadError(c, "middleware.aggregate_route", readErr) {
				AbortWithRequestBodyReadFailure(c, "middleware.aggregate_route", readErr)
			}
			return nil, false
		}
		requestmodel.ResetRequestBody(c.Request, body)
		models := requestmodel.FromBodyCandidates(c.FullPath(), c.GetHeader("Content-Type"), body)
		for _, candidate := range models {
			if model != "" && model != candidate {
				return fail(infraerrors.BadRequest("AGGREGATE_MODEL_AMBIGUOUS", "Request must identify one unambiguous model"))
			}
			model = candidate
		}
		stateful = stateful || gjson.GetBytes(body, "previous_response_id").String() != "" || gjson.GetBytes(body, "cachedContent").Exists()
	}
	model = strings.TrimPrefix(model, "models/")
	if len(scope.AggregateGroups) == 1 && hint == 0 {
		hint = scope.AggregateGroups[0].ID
	}
	if hint == 0 && (model == "" || stateful) {
		return fail(infraerrors.BadRequest("AGGREGATE_GROUP_REQUIRED", "Specify X-Composite-Group-ID for resources, continuations or requests without a model"))
	}
	protocol := service.CompositeNativeProtocolForPath(path)
	if strings.Contains(path, "/v1beta/") {
		protocol = "gemini"
	}
	bound, err := svc.BindAggregateGroup(c.Request.Context(), scope, model, protocol, hint)
	if err != nil {
		return fail(err)
	}
	c.Request = c.Request.WithContext(service.WithAggregateGroup(c.Request.Context(), bound.Group.ID))
	if protocol != "" {
		c.Request = c.Request.WithContext(service.WithCompositeNativeProtocol(c.Request.Context(), protocol))
	}
	c.Header("X-Composite-Group-ID", fmt.Sprint(bound.Group.ID))
	return bound, true
}
