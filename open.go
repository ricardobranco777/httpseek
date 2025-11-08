package httpseek

import (
	"io"
	"net/http"
)

// Open opens a remote HTTP resource as a seekable, readable file.
// It mirrors os.Open in spirit: the resource is opened read-only
// and must be closed when no longer needed.
func Open(url string) (*HTTPFile, error) {
	client := &http.Client{
		Transport: &CachedBlockTransport{
			Transport: http.DefaultTransport,
			Cache:     NewMemoryBlockCache(),
		},
	}
	ra, err := NewReaderAt(url, client)
	if err != nil {
		return nil, err
	}
	return &HTTPFile{ReaderAtHTTP: ra}, nil
}

// Compile-time interface satisfaction checks
var (
	_ io.Reader     = (*HTTPFile)(nil)
	_ io.Seeker     = (*HTTPFile)(nil)
	_ io.ReadSeeker = (*HTTPFile)(nil)
	_ io.ReaderAt   = (*HTTPFile)(nil)
	_ io.Closer     = (*HTTPFile)(nil)
)
