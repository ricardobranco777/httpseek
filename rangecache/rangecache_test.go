/* SPDX-License-Identifier: BSD-2-Clause */

package rangecache

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// helper RoundTripper
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func newRangeServer() (*httptest.Server, *int64) {
	hitCount := new(int64)
	data := []byte("abcdefghijklmnopqrstuvwxyz")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(hitCount, 1)
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			http.Error(w, "missing range", http.StatusBadRequest)
			return
		}
		var start, end int
		_, _ = fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
		if start < 0 || end >= len(data) {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write(data[start : end+1])
	}))
	return srv, hitCount
}

func TestCachedRangeTransport_CachesResponses(t *testing.T) {
	srv, hitCount := newRangeServer()
	defer srv.Close()

	cache := NewMemoryCache()
	client := &http.Client{
		Transport: &CachedRangeTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
		},
	}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Range", "bytes=0-3")

	// first request -> cache miss
	resp1, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if string(body1) != "abcd" {
		t.Fatalf("unexpected body: %q", body1)
	}
	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected 1 server hit, got %d", hitCount)
	}

	// second identical request -> should hit cache
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	if !bytes.Equal(body1, body2) {
		t.Fatalf("cached response mismatch: %q vs %q", body1, body2)
	}
	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected cached hit (no new requests), got %d", hitCount)
	}
}

func TestCachedRangeTransport_PassesThroughOnNonGET(t *testing.T) {
	called := false
	tr := &CachedRangeTransport{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString("ok")),
				Request:    req,
			}, nil
		}),
		Cache: NewMemoryCache(),
	}

	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !called {
		t.Fatal("transport not called for non-GET request")
	}
}

func TestCachedRangeTransport_CacheMissDifferentRange(t *testing.T) {
	srv, hitCount := newRangeServer()
	defer srv.Close()

	cache := NewMemoryCache()
	client := &http.Client{
		Transport: &CachedRangeTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
		},
	}

	reqA, _ := http.NewRequest("GET", srv.URL, nil)
	reqA.Header.Set("Range", "bytes=0-3")
	reqB, _ := http.NewRequest("GET", srv.URL, nil)
	reqB.Header.Set("Range", "bytes=4-7")

	respA, _ := client.Do(reqA)
	io.Copy(io.Discard, respA.Body)
	respA.Body.Close()

	respB, _ := client.Do(reqB)
	io.Copy(io.Discard, respB.Body)
	respB.Body.Close()

	if atomic.LoadInt64(hitCount) != 2 {
		t.Fatalf("expected 2 server hits for distinct ranges, got %d", hitCount)
	}
}

func TestCachedRangeTransport_ClearRemovesEntries(t *testing.T) {
	srv, hitCount := newRangeServer()
	defer srv.Close()

	cache := NewMemoryCache()
	client := &http.Client{
		Transport: &CachedRangeTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
		},
	}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Range", "bytes=0-3")

	resp1, _ := client.Do(req)
	io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()

	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected 1 hit before Clear, got %d", hitCount)
	}

	cache.Clear()

	resp2, _ := client.Do(req)
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()

	if atomic.LoadInt64(hitCount) != 2 {
		t.Fatalf("expected cache cleared -> new request, got %d", hitCount)
	}
}

func TestCachedRangeTransport_NilCachePassthrough(t *testing.T) {
	called := false
	tr := &CachedRangeTransport{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Body:       io.NopCloser(bytes.NewBufferString("abcd")),
				Request:    req,
			}, nil
		}),
		Cache: nil,
	}
	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Range", "bytes=0-3")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if !called {
		t.Fatal("transport not called with nil cache")
	}
}

func TestCachedRangeTransport_Non206ResponseIsNotCached(t *testing.T) {
	hitCount := new(int64)
	tr := &CachedRangeTransport{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt64(hitCount, 1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("fullbody")),
				Request:    req,
			}, nil
		}),
		Cache: NewMemoryCache(),
	}
	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Range", "bytes=0-3")

	for i := 0; i < 2; i++ {
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	if atomic.LoadInt64(hitCount) != 2 {
		t.Fatalf("expected no caching for non-206 responses, got %d hits", hitCount)
	}
}

func TestCachedRangeTransport_ConcurrentAccess(t *testing.T) {
	srv, hitCount := newRangeServer()
	defer srv.Close()

	cache := NewMemoryCache()
	client := &http.Client{
		Transport: &CachedRangeTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
		},
	}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Range", "bytes=0-3")

	// Fill cache with one request first
	client.Do(req)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("request error: %v", err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()

	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected concurrent cache hit after first fetch, got %d", hitCount)
	}
}
