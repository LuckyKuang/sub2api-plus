package brandidentity

import (
	"net/http"

	"github.com/imroc/req/v3"
)

// WrapReqClient checks the completed request after req has assembled defaults,
// cookies and per-request headers, including each redirected attempt.
func WrapReqClient(client *req.Client) *req.Client {
	client.GetTransport().WrapRoundTripFunc(func(base http.RoundTripper) req.HttpRoundTripFunc {
		return WrapRoundTripper(base).RoundTrip
	})
	return client
}
