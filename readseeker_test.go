/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveBytesRange returns an httptest.Server that supports Range and HEAD requests.
func serveBytesRange(data []byte) *httptest.Server {
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

func TestReadSeeker_SequentialReads(t *testing.T) {
	data := []byte("abcdefghijklmnopqrstuvwxyz")
	srv := serveBytesRange(data)
	defer srv.Close()

	ra, err := NewReaderAt(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := NewReadSeeker(ra)
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

func TestReadSeeker_SeekFromStart(t *testing.T) {
	data := []byte("0123456789abcdef")
	srv := serveBytesRange(data)
	defer srv.Close()

	ra, _ := NewReaderAt(srv.URL, nil)
	r := NewReadSeeker(ra)
	defer r.Close()

	r.Seek(8, io.SeekStart)
	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	want := data[8:12]
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("seek mismatch: got %q want %q", buf[:n], want)
	}
}

func TestReadSeeker_SeekCurrentAndBackwards(t *testing.T) {
	data := []byte("abcdefghijk")
	srv := serveBytesRange(data)
	defer srv.Close()

	ra, _ := NewReaderAt(srv.URL, nil)
	r := NewReadSeeker(ra)
	defer r.Close()

	buf := make([]byte, 4)

	// Read first 4 bytes -> offset = 4
	r.Read(buf)

	// Seek backwards by 2 bytes -> offset = 2
	newOff, err := r.Seek(-2, io.SeekCurrent)
	if err != nil {
		t.Fatal(err)
	}
	if newOff != 2 {
		t.Fatalf("expected offset 2, got %d", newOff)
	}

	// Read next 4 bytes starting from offset 2 -> expect "cdef"
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	if got, want := string(buf[:n]), "cdef"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReadSeeker_SeekFromEnd(t *testing.T) {
	data := []byte("abcdef")
	srv := serveBytesRange(data)
	defer srv.Close()

	ra, _ := NewReaderAt(srv.URL, nil)
	r := NewReadSeeker(ra)
	defer r.Close()

	// Seek to 2 bytes before end
	off, err := r.Seek(-2, io.SeekEnd)
	if err != nil {
		t.Fatal(err)
	}
	if off != int64(len(data)-2) {
		t.Fatalf("expected offset %d, got %d", len(data)-2, off)
	}

	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	if got, want := string(buf[:n]), "ef"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReadSeeker_SeekInvalid(t *testing.T) {
	data := []byte("abc")
	srv := serveBytesRange(data)
	defer srv.Close()

	ra, _ := NewReaderAt(srv.URL, nil)
	r := NewReadSeeker(ra)
	defer r.Close()

	if _, err := r.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("expected error for negative offset")
	}
	if _, err := r.Seek(0, 99); err == nil {
		t.Fatal("expected error for invalid whence")
	}
}

func TestReadSeeker_ReadEOF(t *testing.T) {
	data := []byte("xyz")
	srv := serveBytesRange(data)
	defer srv.Close()

	ra, _ := NewReaderAt(srv.URL, nil)
	r := NewReadSeeker(ra)
	defer r.Close()

	r.Seek(int64(len(data)), io.SeekStart)
	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if n != 0 || err != io.EOF {
		t.Fatalf("expected EOF, got n=%d err=%v", n, err)
	}
}
