package httpseek

import (
	"errors"
	"io"
	"sync"
)

var ErrInvalidSeek = errors.New("invalid seek")

// Reader implements io.ReadSeeker on top of ReaderAtHTTP.
type Reader struct {
	*ReaderAtHTTP
	off int64
	mu  sync.Mutex
}

// NewReadSeeker wraps an existing ReaderAtHTTP.
func NewReadSeeker(r *ReaderAtHTTP) *Reader {
	return &Reader{ReaderAtHTTP: r}
}

// Read reads from the current offset and advances it.
func (r *Reader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, err := r.ReadAt(p, r.off)
	r.off += int64(n)
	return n, err
}

// Seek implements io.Seeker.
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var newOff int64
	switch whence {
	case io.SeekStart:
		newOff = offset
	case io.SeekCurrent:
		newOff = r.off + offset
	case io.SeekEnd:
		newOff = r.Size() + offset
	default:
		return 0, ErrInvalidSeek
	}

	if newOff < 0 {
		return 0, ErrInvalidSeek
	}
	r.off = newOff
	return r.off, nil
}
