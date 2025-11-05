package httpseek

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"

	"github.com/ricardobranco777/httpseek/internal/logutil"
)

type Logger interface {
	Debug(msg string, args ...any)
	Error(msg string, args ...any)
}

// ReaderAtHTTP implements io.ReaderAt for HTTP URLs using Range requests.
type ReaderAtHTTP struct {
	client *http.Client
	logger Logger
	size   int64
	url    string
}

// NewReaderAt creates a ReaderAtHTTP. If client is nil, http.DefaultClient is used.
func NewReaderAt(url string, client *http.Client) (*ReaderAtHTTP, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("httpseek: HEAD %s returned %s", url, resp.Status)
	}

	cl := resp.Header.Get("Content-Length")
	if cl == "" {
		return nil, fmt.Errorf("httpseek: missing Content-Length")
	}

	size, err := strconv.ParseInt(cl, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("httpseek: invalid Content-Length: %w", err)
	}

	if !strings.Contains(resp.Header.Get("Accept-Ranges"), "bytes") {
		return nil, fmt.Errorf("httpseek: server does not support Range requests")
	}

	logger := logutil.NoopLogger()
	return &ReaderAtHTTP{
		client: client,
		logger: logger,
		size:   size,
		url:    url,
	}, nil
}

// ReadAt issues a GET request with Range: bytes=off-(off+len(p)-1).
func (r *ReaderAtHTTP) ReadAt(p []byte, off int64) (int, error) {
	if off >= r.size {
		return 0, io.EOF
	}

	end := off + int64(len(p)) - 1
	if end >= r.size {
		end = r.size - 1
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", r.url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))

	if dump, err := httputil.DumpRequestOut(req, true); err == nil {
		r.logger.Debug("", string(dump))
	} else {
		r.logger.Error("Failed to dump request", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if dump, err := httputil.DumpResponse(resp, true); err == nil {
		r.logger.Debug("", string(dump))
	} else {
		r.logger.Error("Failed to dump response", err)
	}

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("httpseek: unexpected HTTP status %s", resp.Status)
	}

	n, err := io.ReadFull(resp.Body, p[:end-off+1])
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return n, err
}

// Size returns the content length.
func (r *ReaderAtHTTP) Size() int64 { return r.size }

// Close is a no-op for interface compatibility.
func (r *ReaderAtHTTP) Close() error { return nil }

// SetLogger sets an optional logger for debug output.
// If nil, no logs are emitted.
func (r *ReaderAtHTTP) SetLogger(l Logger) {
	r.logger = l
}
