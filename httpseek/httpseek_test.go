/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serveBytes creates an httptest.Server that supports Range requests for given data.
func serveBytes(data []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "HEAD":
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
		case "GET":
			// Validate precondition headers (simulate change)
			ifMatch := r.Header.Get("If-Match")
			if ifMatch != "" && ifMatch != `"v1"` {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			ifUnmod := r.Header.Get("If-Unmodified-Since")
			if ifUnmod != "" && strings.Contains(ifUnmod, "old") {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}

			rangeHdr := r.Header.Get("Range")
			if rangeHdr == "" {
				http.Error(w, "Range required", http.StatusBadRequest)
				return
			}
			var start, end int
			n, _ := fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
			if n != 2 {
				http.Error(w, "Bad Range", http.StatusBadRequest)
				return
			}
			if start < 0 || end >= len(data) || start > end {
				http.Error(w, "Invalid Range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(data[start : end+1])
		default:
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		}
	}))
}

func TestReaderAtBasic(t *testing.T) {
	data := []byte("abcdefghijklmnopqrstuvwxyz")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := NewReaderAt(srv.URL, nil)
	if err != nil {
		t.Fatalf("NewReaderAt: %v", err)
	}
	defer r.Close()

	buf := make([]byte, 5)
	n, err := r.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(buf[:n], data[:5]) {
		t.Fatalf("unexpected data: got %q want %q", buf[:n], data[:5])
	}
}

func TestReaderAtOffset(t *testing.T) {
	data := []byte("0123456789abcdef")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := NewReaderAt(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 4)
	n, err := r.ReadAt(buf, 4)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	want := data[4:8]
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("offset mismatch: got %q want %q", buf[:n], want)
	}
}

func TestReaderAtBeyondEOF(t *testing.T) {
	data := []byte("xyz")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := NewReaderAt(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 8)
	n, err := r.ReadAt(buf, int64(len(data))) // start exactly at EOF
	if n != 0 || err != io.EOF {
		t.Fatalf("expected EOF at EOF, got n=%d err=%v", n, err)
	}
}

func TestReaderAt_PreconditionFailed_ETag(t *testing.T) {
	data := []byte("0123456789")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := NewReaderAt(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate remote file change
	r.etag = `"v2"`

	buf := make([]byte, 4)
	_, err = r.ReadAt(buf, 0)
	if err == nil || !strings.Contains(err.Error(), "remote resource changed") {
		t.Fatalf("expected precondition failure, got %v", err)
	}
}

func TestReaderAt_PreconditionFailed_LastModified(t *testing.T) {
	data := []byte("0123456789")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := NewReaderAt(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate stale last-modified date
	r.lastMod = "Thu, 01 Jan 1970 00:00:00 old"

	buf := make([]byte, 4)
	_, err = r.ReadAt(buf, 0)
	if err == nil || !strings.Contains(err.Error(), "remote resource changed") {
		t.Fatalf("expected precondition failure, got %v", err)
	}
}

func TestReaderAt_ServerWithoutRangeSupport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		// no Accept-Ranges
	}))
	defer srv.Close()

	_, err := NewReaderAt(srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "does not support Range") {
		t.Fatalf("expected missing range support error, got %v", err)
	}
}

func TestReaderAt_MissingContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	_, err := NewReaderAt(srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "missing Content-Length") {
		t.Fatalf("expected missing Content-Length error, got %v", err)
	}
}

func TestReaderAt_HeadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := NewReaderAt(srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "returned 403") {
		t.Fatalf("expected non-2xx HEAD failure, got %v", err)
	}
}
