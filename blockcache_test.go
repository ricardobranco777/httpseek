/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

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

// newBlockServer simulates a Range-capable HTTP server.
func newBlockServer() (*httptest.Server, *int64) {
	hitCount := new(int64)
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(hitCount, 1)

		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			http.Error(w, "Range required", http.StatusBadRequest)
			return
		}

		var start, end int
		_, _ = fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
		if start < 0 || end >= len(data) || start > end {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(data[start : end+1])
	}))
	return srv, hitCount
}

func TestCachedBlockTransport_BasicCaching(t *testing.T) {
	srv, hitCount := newBlockServer()
	defer srv.Close()

	cache := NewMemoryBlockCache()
	client := &http.Client{
		Transport: &CachedBlockTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
			BlockSize: 512,
		},
	}

	// Request within first block
	req1, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req1.Header.Set("Range", "bytes=0-127")
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if len(body1) != 128 {
		t.Fatalf("expected 128 bytes, got %d", len(body1))
	}
	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected 1 server hit, got %d", atomic.LoadInt64(hitCount))
	}

	// Request another subrange within same block (should hit cache)
	req2, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req2.Header.Set("Range", "bytes=100-200")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected cache hit, got new server hit (%d)", atomic.LoadInt64(hitCount))
	}
	if len(body2) != 101 {
		t.Fatalf("expected 101 bytes, got %d", len(body2))
	}
}

func TestCachedBlockTransport_MultipleBlocks(t *testing.T) {
	srv, hitCount := newBlockServer()
	defer srv.Close()

	cache := NewMemoryBlockCache()
	client := &http.Client{
		Transport: &CachedBlockTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
			BlockSize: 512,
		},
	}

	// Request from block 0
	reqA, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	reqA.Header.Set("Range", "bytes=0-255")
	respA, _ := client.Do(reqA)
	io.Copy(io.Discard, respA.Body)
	respA.Body.Close()

	// Request from block 1
	reqB, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	reqB.Header.Set("Range", "bytes=600-700")
	respB, _ := client.Do(reqB)
	io.Copy(io.Discard, respB.Body)
	respB.Body.Close()

	if atomic.LoadInt64(hitCount) != 2 {
		t.Fatalf("expected 2 block fetches, got %d", atomic.LoadInt64(hitCount))
	}

	// Request again from block 0 (cache hit)
	reqC, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	reqC.Header.Set("Range", "bytes=100-200")
	respC, _ := client.Do(reqC)
	io.Copy(io.Discard, respC.Body)
	respC.Body.Close()

	if atomic.LoadInt64(hitCount) != 2 {
		t.Fatalf("expected cache reuse for block 0, got %d", atomic.LoadInt64(hitCount))
	}
}

func TestCachedBlockTransport_UnalignedRequest(t *testing.T) {
	srv, hitCount := newBlockServer()
	defer srv.Close()

	cache := NewMemoryBlockCache()
	client := &http.Client{
		Transport: &CachedBlockTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
			BlockSize: 512,
		},
	}

	// Request not aligned to block boundaries
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Range", "bytes=400-550")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if len(data) != 151 {
		t.Fatalf("expected 151 bytes, got %d", len(data))
	}
	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected 1 network request, got %d", atomic.LoadInt64(hitCount))
	}
}

func TestCachedBlockTransport_FinalPartialBlock(t *testing.T) {
	srv, hitCount := newBlockServer()
	defer srv.Close()

	cache := NewMemoryBlockCache()
	client := &http.Client{
		Transport: &CachedBlockTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
			BlockSize: 512,
		},
	}

	// Request that falls inside the final (partial) block
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Range", "bytes=3584-4095") // Last 512 bytes, but only 4096 total
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if len(body) != 512 {
		t.Fatalf("expected final block of 512 bytes, got %d", len(body))
	}
	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected 1 fetch, got %d", atomic.LoadInt64(hitCount))
	}
}

func TestCachedBlockTransport_Singleflight(t *testing.T) {
	srv, hitCount := newBlockServer()
	defer srv.Close()

	cache := NewMemoryBlockCache()
	client := &http.Client{
		Transport: &CachedBlockTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
			BlockSize: 512,
		},
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Range", "bytes=0-100")

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("error: %v", err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		})
	}
	wg.Wait()

	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected singleflight deduplication, got %d hits", atomic.LoadInt64(hitCount))
	}
}

func TestCachedBlockTransport_BlockSize_Default(t *testing.T) {
	srv, hitCount := newBlockServer()
	defer srv.Close()

	cache := NewMemoryBlockCache()
	client := &http.Client{
		Transport: &CachedBlockTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
			BlockSize: 0, // will default internally
		},
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Range", "bytes=0-100")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected one request, got %d", atomic.LoadInt64(hitCount))
	}
	if _, ok := cache.Get(0); !ok {
		t.Fatal("expected block 0 to be cached")
	}
}
func TestCachedBlockTransport_PassesThroughOnNonGET(t *testing.T) {
	called := false
	tr := &CachedBlockTransport{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("ok")),
				Request:    req,
			}, nil
		}),
		Cache:     NewMemoryBlockCache(),
		BlockSize: 512,
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

func TestCachedBlockTransport_NilCachePassthrough(t *testing.T) {
	called := false
	tr := &CachedBlockTransport{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Body:       io.NopCloser(bytes.NewBufferString("abcd")),
				Request:    req,
			}, nil
		}),
		Cache:     nil,
		BlockSize: 512,
	}
	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set("Range", "bytes=0-3")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if !called {
		t.Fatal("transport not called when cache is nil")
	}
}

func TestCachedBlockTransport_ClearRemovesEntries(t *testing.T) {
	srv, hitCount := newBlockServer()
	defer srv.Close()

	cache := NewMemoryBlockCache()
	client := &http.Client{
		Transport: &CachedBlockTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
			BlockSize: 512,
		},
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Range", "bytes=0-127")
	resp1, _ := client.Do(req)
	io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()

	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected 1 hit before Clear, got %d", atomic.LoadInt64(hitCount))
	}

	cache.Clear()

	resp2, _ := client.Do(req)
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()

	if atomic.LoadInt64(hitCount) != 2 {
		t.Fatalf("expected new request after Clear, got %d", atomic.LoadInt64(hitCount))
	}
}

func TestCachedBlockTransport_Non206ResponseIsNotCached(t *testing.T) {
	hitCount := new(int64)
	tr := &CachedBlockTransport{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt64(hitCount, 1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("wholefile")),
				Request:    req,
			}, nil
		}),
		Cache:     NewMemoryBlockCache(),
		BlockSize: 512,
	}
	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set("Range", "bytes=0-127")

	for range 2 {
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected no caching for non-206 responses, got %d hits", atomic.LoadInt64(hitCount))
	}
}

func TestCachedBlockTransport_ConcurrentAccess(t *testing.T) {
	srv, hitCount := newBlockServer()
	defer srv.Close()

	cache := NewMemoryBlockCache()
	client := &http.Client{
		Transport: &CachedBlockTransport{
			Transport: http.DefaultTransport,
			Cache:     cache,
			BlockSize: 512,
		},
	}

	// Warm cache
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Range", "bytes=0-255")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			r, err := http.NewRequest(http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Error(err)
				return
			}
			r.Header.Set("Range", "bytes=0-127")
			resp, err := client.Do(r)
			if err != nil {
				t.Error(err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		})
	}
	wg.Wait()

	if atomic.LoadInt64(hitCount) != 1 {
		t.Fatalf("expected cache reuse for concurrent access, got %d hits", atomic.LoadInt64(hitCount))
	}
}

// helper RoundTripper
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCachedBlockTransport_ErrorsDoNotCache(t *testing.T) {
	called := int64(0)
	tr := &CachedBlockTransport{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt64(&called, 1)
			return nil, fmt.Errorf("simulated network error")
		}),
		Cache:     NewMemoryBlockCache(),
		BlockSize: 512,
	}

	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set("Range", "bytes=0-127")

	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected simulated error")
	}

	// Try again — should reattempt, not serve from cache
	_, _ = client.Do(req)
	if atomic.LoadInt64(&called) != 2 {
		t.Fatalf("expected no caching after errors, got %d", called)
	}
}

func TestMemoryBlockCache_BasicOps(t *testing.T) {
	cache := NewMemoryBlockCache()
	data := []byte("hello")

	cache.Put(1, data)
	if got, ok := cache.Get(1); !ok || string(got) != "hello" {
		t.Fatalf("expected to get 'hello', got %q (ok=%v)", got, ok)
	}

	cache.Delete(1)
	if _, ok := cache.Get(1); ok {
		t.Fatal("expected deleted entry to be gone")
	}

	cache.Put(2, []byte("world"))
	cache.Clear()
	if _, ok := cache.Get(2); ok {
		t.Fatal("expected cache clear to remove entries")
	}
}
