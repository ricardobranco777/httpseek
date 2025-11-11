// SPDX-License-Identifier: BSD-2-Clause
package httpseek

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HTTPFile implements io.ReaderAt and io.ReadSeekCloser and io.ReaderAt using HTTP Range requests.
type HTTPFile struct {
	client *http.Client
	meta   Metadata
	off    int64
	url    string
}

// HTTPFile satisfies these interfaces:
var (
	_ io.ReaderAt       = (*HTTPFile)(nil)
	_ io.ReadSeeker     = (*HTTPFile)(nil)
	_ io.ReadSeekCloser = (*HTTPFile)(nil)
)

// New returns a HTTPFile. If client is nil, http.DefaultClient is used.
func New(url string, client *http.Client) (*HTTPFile, error) {
	if client == nil {
		client = http.DefaultClient
	}

	// HEAD request to determine file size and range support
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return nil, err
	}

	logRequest(req, true)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	logResponse(resp, true)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("httpseek: HEAD %s returned %s", url, resp.Status)
	}

	meta := extractMetadata(resp.Header)
	if meta.Length <= 0 {
		return nil, fmt.Errorf("httpseek: missing Content-Length")
	}

	if !strings.Contains(resp.Header.Get("Accept-Ranges"), "bytes") {
		return nil, errors.New("httpseek: server does not accept bytes ranges")
	}

	return &HTTPFile{
		client: client,
		meta:   meta,
		url:    url,
	}, nil
}

// Open returns a HTTPFile.
func Open(url string, client *http.Client) (*HTTPFile, error) {
	return New(url, nil)
}

// Close closes the file.
func (r *HTTPFile) Close() error {
	return nil
}

// Read issues a GET with Range corresponding to the current offset.
func (r *HTTPFile) Read(p []byte) (int, error) {
	n, err := r.ReadAt(p, r.off)
	if err == nil || errors.Is(err, io.EOF) {
		r.off += int64(n)
	}
	return n, err
}

// ReadAt reads exactly len(p) bytes from offset offset.
// It does not affect the current seek position and is safe for concurrent use.
func (r *HTTPFile) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, errors.New("httpseek: invalid offset")
	}
	if offset >= r.meta.Length {
		return 0, io.EOF
	}

	end := offset + int64(len(p)) - 1
	if end >= r.meta.Length {
		end = r.meta.Length - 1
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, r.url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end))
	r.meta.ApplyValidators(req.Header)

	logRequest(req, true)

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	logResponse(resp, false)

	switch resp.StatusCode {
	case http.StatusPartialContent, http.StatusOK:
	default:
		return 0, fmt.Errorf("httpseek: unexpected HTTP status %s", resp.Status)
	}

	meta := extractMetadata(resp.Header)
	if !r.meta.Equal(meta) {
		return 0, fmt.Errorf("httpseek: metadata mismatch")
	}

	n, err := io.ReadFull(resp.Body, p[:end-offset+1])
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return n, err
}

// Seek sets the offset for the next Read.
func (r *HTTPFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.off
	case io.SeekEnd:
		offset += r.meta.Length
	default:
		return 0, errors.New("httpseek: invalid whence")
	}
	if offset < 0 {
		return 0, errors.New("httpseek: invalid offset")
	}
	r.off = offset
	return r.off, nil
}

// Size returns the remote file size in bytes.
func (r *HTTPFile) Size() int64 {
	return r.meta.Length
}
