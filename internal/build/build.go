// Package build exposes build-time metadata populated via -ldflags plus
// constants that identify this service in spans, logs, and OpenAPI metadata.
package build

// ServiceName is the canonical identifier for this service. Used as the OTel
// resource attribute service.name and the otelgin span service name.
const ServiceName = "graph-api-gateway"

// Version is the build version, overridden via -ldflags at build time.
var Version = "dev"

// Commit is the build commit SHA, overridden via -ldflags at build time.
var Commit = "none"
