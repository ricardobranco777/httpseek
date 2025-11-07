package httpseek

import (
	"io"
	"net/http"
)

// API contract compile-time checks.
var (
	_ io.ReaderAt    = (*ReaderAtHTTP)(nil)
	_ io.ReadSeeker  = (*Reader)(nil)
	_ io.Closer      = (*Reader)(nil)
	_ http.RoundTripper = (*CachedRangeTransport)(nil)
)
