package client

import (
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
)

// newHTTPClient returns an *http.Client whose transport is wrapped by otelhttp
// so every outbound request opens a client span and injects W3C traceparent.
// Propagators are pinned at the transport level so the client works even when
// no global propagator has been installed (e.g., unit tests).
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: otelhttp.NewTransport(http.DefaultTransport,
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return r.Method + " " + r.URL.Path
			}),
			otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator(
				propagation.TraceContext{},
				propagation.Baggage{},
			)),
		),
	}
}
