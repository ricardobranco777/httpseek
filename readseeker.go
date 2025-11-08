package httpseek

import (
	"context"
	"errors"
	"io"
	"sync"
)

var ErrInvalidSeek = errors.New("invalid seek")

// HTTPFile provides a file-like abstraction for HTTP resources.
// It implements io.ReadSeeker, io.ReaderAt, and io.Closer.
// Internally, it uses ReaderAtHTTP for range-based access.
type HTTPFile struct {
	*ReaderAtHTTP
	offset int64
	mu     sync.Mutex
}

// NewReadSeeker wraps an existing ReaderAtHTTP.
func NewReadSeeker(r *ReaderAtHTTP) *HTTPFile {
	return &HTTPFile{ReaderAtHTTP: r}
}

// Read reads from the current offset and advances it.
func (r *HTTPFile) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, err := r.ReadAt(p, r.offset)
	r.offset += int64(n)
	return n, err
}

// Seek implements io.Seeker.
func (r *HTTPFile) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.offset
	case io.SeekEnd:
		offset += r.Size()
	default:
		return 0, ErrInvalidSeek
	}

	if offset < 0 {
		return 0, ErrInvalidSeek
	}
	r.offset = offset
	return r.offset, nil
}

// ReadAtContext is like ReadAt with context.
func (f *HTTPFile) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	return f.ReaderAtHTTP.ReadAtContext(ctx, p, off)
}
