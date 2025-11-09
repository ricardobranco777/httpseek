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
)

// serveBytes creates an httptest.Server that supports Range requests for given data.
func serveBytes(data []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
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

	r, err := New(srv.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
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

	r, err := New(srv.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	buf := make([]byte, 4)
	n, err := r.ReadAt(buf, 4)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt: %v", err)
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

	r, err := New(srv.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	buf := make([]byte, 8)
	n, err := r.ReadAt(buf, int64(len(data))) // start exactly at EOF
	if n != 0 || err != io.EOF {
		t.Fatalf("expected EOF at EOF, got n=%d err=%v", n, err)
	}
}

func TestReaderAtServerWithoutRangeSupport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		// no Accept-Ranges
	}))
	defer srv.Close()

	_, err := New(srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "server does not accept bytes range") {
		t.Fatalf("expected server does not accept bytes range, got %v", err)
	}
}

func TestReaderAtMissingContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	_, err := New(srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "missing Content-Length") {
		t.Fatalf("expected missing Content-Length error, got %v", err)
	}
}

func TestReaderAtHeadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := New(srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "returned 403") {
		t.Fatalf("expected non-2xx HEAD failure, got %v", err)
	}
}

func TestReadSeekerSequentialReads(t *testing.T) {
	data := []byte("abcdefghijklmnopqrstuvwxyz")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := New(srv.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	buf := make([]byte, 5)
	var total []byte

	for {
		n, err := r.Read(buf)
		total = append(total, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
	}

	if !bytes.Equal(total, data[:len(total)]) {
		t.Fatalf("unexpected data: got %q want %q", total, data)
	}
}

func TestReadSeekerSeekFromStart(t *testing.T) {
	data := []byte("0123456789abcdef")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := New(srv.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	r.Seek(8, io.SeekStart)
	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read: %v", err)
	}

	want := data[8:12]
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("seek mismatch: got %q want %q", buf[:n], want)
	}
}

func TestReadSeekerSeekCurrentAndBackwards(t *testing.T) {
	data := []byte("abcdefghijk")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := New(srv.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	buf := make([]byte, 4)

	// Read first 4 bytes -> offset = 4
	r.Read(buf)

	// Seek backwards by 2 bytes -> offset = 2
	newOff, err := r.Seek(-2, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if newOff != 2 {
		t.Fatalf("expected offset 2, got %d", newOff)
	}

	// Read next 4 bytes starting from offset 2 -> expect "cdef"
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read: %v", err)
	}

	if got, want := string(buf[:n]), "cdef"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReadSeekerSeekFromEnd(t *testing.T) {
	data := []byte("abcdef")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := New(srv.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	// Seek to 2 bytes before end
	off, err := r.Seek(-2, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if off != int64(len(data)-2) {
		t.Fatalf("expected offset %d, got %d", len(data)-2, off)
	}

	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read: %v", err)
	}

	if got, want := string(buf[:n]), "ef"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReadSeekerSeekInvalid(t *testing.T) {
	data := []byte("abc")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := New(srv.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	if _, err := r.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("expected error for negative offset")
	}
	if _, err := r.Seek(0, 99); err == nil {
		t.Fatal("expected error for invalid whence")
	}
}

func TestReadSeekerReadEOF(t *testing.T) {
	data := []byte("xyz")
	srv := serveBytes(data)
	defer srv.Close()

	r, err := New(srv.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	r.Seek(int64(len(data)), io.SeekStart)
	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if n != 0 || err != io.EOF {
		t.Fatalf("expected EOF, got n=%d err=%v", n, err)
	}
}
