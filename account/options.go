package account

// CallOption customizes a single per-call invocation. Functional
// per-call options follow the same pattern as zyins.RunOption so
// callers see one consistent idiom across the SDK.
type CallOption func(*callOptions)

type callOptions struct {
	idempotencyKey string
}

// WithIdempotencyKey overrides the SDK's auto-derived Idempotency-Key
// header for one call. Useful when the caller wants the same key across
// an external retry loop.
func WithIdempotencyKey(key string) CallOption {
	return func(o *callOptions) { o.idempotencyKey = key }
}

func collectCallOptions(opts []CallOption) callOptions {
	co := callOptions{}
	for _, o := range opts {
		if o != nil {
			o(&co)
		}
	}
	return co
}
