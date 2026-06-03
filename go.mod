// Unified ISA SDK for Go.
//
// Published as a single module: github.com/isa-sdk/sdk
//
// Layout:
//
//	core/      — Auth, errors, Envelope, transport, debug, replay
//	zyins/     — Prequalify, Quote, datasets, namespace
//	rapidsign/ — Documents, webhooks, awaitSignature
//	proxy/     — Algosure verifier, raw-call transport
//
// Consumers depend on the root module and import sub-packages:
//
//	import (
//	    sdk "github.com/isa-sdk/sdk"
//	    "github.com/isa-sdk/sdk/zyins"
//	)
//
// See SDK_DESIGN.md §0 for consolidation rationale.

module github.com/isa-sdk/sdk

go 1.26.1

require golang.org/x/sync v0.20.0

require (
	connectrpc.com/connect v1.20.0
	golang.org/x/text v0.37.0
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa
	google.golang.org/protobuf v1.36.11
)
