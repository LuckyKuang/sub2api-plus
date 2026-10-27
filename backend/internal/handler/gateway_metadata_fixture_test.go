//go:build unit || !integration

package handler

import (
	"context"

	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

// Catalog tests explicitly provide authenticated user admission, just as
// production construction does; a missing production helper remains an error.
type metadataConcurrencyCache struct{ service.ConcurrencyCache }

func (*metadataConcurrencyCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (*metadataConcurrencyCache) ReleaseUserSlot(context.Context, int64, string) error { return nil }
func (*metadataConcurrencyCache) GetUserConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}

func prepareGatewayMetadataTest(h *GatewayHandler, c *gin.Context) {
	if _, ok := middleware.GetAuthSubjectFromContext(c); !ok {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 3})
	}
	if h.concurrencyHelper == nil {
		h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(&metadataConcurrencyCache{}), SSEPingFormatNone, 0)
	}
}

func prepareOpenAIMetadataTest(h *OpenAIGatewayHandler, c *gin.Context) {
	if _, ok := middleware.GetAuthSubjectFromContext(c); !ok {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 3})
	}
	if h.concurrencyHelper == nil {
		h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(&metadataConcurrencyCache{}), SSEPingFormatNone, 0)
	}
}
