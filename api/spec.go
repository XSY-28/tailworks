// Package api holds the /v1 contract, api/openapi.yaml, the source of truth
// for the generated server (mgmt/api/v1), client (hostd/mgmtapi/v1) and the
// rendered docs/contracts/mgmt-api.md.
//
// The spec is embedded here as plain YAML instead of oapi-codegen's
// embedded-spec blob: that blob is gzip+base64, so every spec change
// rewrote hundreds of lines of api.gen.go and parallel branches always
// conflicted there.
package api

import _ "embed"

// OpenAPI is api/openapi.yaml, byte for byte.
//
//go:embed openapi.yaml
var OpenAPI []byte
