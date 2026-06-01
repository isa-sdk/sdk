package replay

import "errors"

// errInvalidWindow is returned by NewInMemoryCache when Window <= 0.
// Kept unexported because callers should not branch on this — it is a
// programmer error surfaced at construction time, not at runtime.
var errInvalidWindow = errors.New("replay: Window must be positive")
