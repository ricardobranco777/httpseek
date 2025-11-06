/* SPDX-License-Identifier: BSD-2-Clause */

package rangecache

import (
	"bytes"
	"io"
	"net/http"
	"sync"

	"golang.org/x/sync/singleflight"
)

// Cache defines a minimal interface for storing range responses.
type Cache interface {
	Clear()
	Delete(key string)
	Get(key string) ([]byte, bool)
	Put(key string, data []byte)
}

// MemoryCache is a simple in-memory implementation.
type MemoryCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{m: make(map[string][]byte)}
}

func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = make(map[string][]byte)
}

func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
}

func (c *MemoryCache) Get(k string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *MemoryCache) Put(k string, v []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
}

// CachedRangeTransport caches HTTP Range GET responses.
type CachedRangeTransport struct {
	Transport http.RoundTripper
	Cache     Cache
	group     singleflight.Group
}

// RoundTrip implements http.RoundTripper with Range caching.
func (t *CachedRangeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Transport == nil {
		t.Transport = http.DefaultTransport
	}

	// Only cache GET requests with a Range header
	if req.Method != "GET" {
		return t.Transport.RoundTrip(req)
	}
	rangeHdr := req.Header.Get("Range")
	if rangeHdr == "" {
		return t.Transport.RoundTrip(req)
	}

	key := req.URL.String() + "|" + rangeHdr

	// Try cache first
	if t.Cache != nil {
		if data, ok := t.Cache.Get(key); ok {
			return &http.Response{
				StatusCode:    http.StatusPartialContent,
				Status:        "206 Partial Content",
				Body:          io.NopCloser(bytes.NewReader(data)),
				ContentLength: int64(len(data)),
				Header:        http.Header{"Content-Range": []string{rangeHdr}},
				Request:       req,
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
			}, nil
		}
	}

	// Use singleflight to prevent duplicate fetches for same key
	v, err, _ := t.group.Do(key, func() (interface{}, error) {
		resp, err := t.Transport.RoundTrip(req)
		if err != nil {
			return nil, err
		}

		// Only cache partial content (Range) responses
		if resp.StatusCode == http.StatusPartialContent {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return nil, err
			}
			if t.Cache != nil {
				t.Cache.Put(key, body)
			}
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			return resp, nil
		}

		// Return as-is if not cacheable
		return resp, nil
	})
	if err != nil {
		return nil, err
	}

	// Return shared result
	return v.(*http.Response), nil
}
