/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ReaderAtHTTP implements io.ReaderAt via HTTP Range requests.
type ReaderAtHTTP struct {
	client *http.Client
	meta   Metadata
	size   int64
	url    string
}

// NewReaderAt creates a ReaderAtHTTP. If client is nil, http.DefaultClient is used.
func NewReaderAt(url string, client *http.Client) (*ReaderAtHTTP, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return nil, err
	}

	logRequest(req)

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

	return &ReaderAtHTTP{
		url:    url,
		client: client,
		size:   size,
		meta:   FromHeaders(resp.Header),
	}, nil
}

// ReadAt issues a conditional GET with Range: bytes=off-(off+len(p)-1).
func (r *ReaderAtHTTP) ReadAt(p []byte, off int64) (int, error) {
	return r.ReadAtContext(context.Background(), p, off)
}

// ReadAt with context
func (r *ReaderAtHTTP) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	if off >= r.size {
		return 0, io.EOF
	}

	end := off + int64(len(p)) - 1
	if end >= r.size {
		end = r.size - 1
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))
	r.meta.ApplyValidators(req.Header)

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent, http.StatusOK:
		// accepted
	case http.StatusPreconditionFailed:
		return 0, fmt.Errorf("httpseek: precondition failed (HTTP 412)")
	default:
		return 0, fmt.Errorf("httpseek: unexpected HTTP status %s", resp.Status)
	}

	// Update metadata if changed
	newMeta := FromHeaders(resp.Header)
	if !r.meta.Equal(newMeta) {
		r.meta = newMeta
	}

	n, err := io.ReadFull(resp.Body, p[:end-off+1])
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return n, err
}

// Size returns the total content length.
func (r *ReaderAtHTTP) Size() int64 { return r.size }

// Close is a no-op for interface compatibility.
func (r *ReaderAtHTTP) Close() error { return nil }

// Compile-time interface satisfaction checks
var _ io.ReaderAt = (*ReaderAtHTTP)(nil)
