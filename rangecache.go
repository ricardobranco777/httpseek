/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"

	"golang.org/x/sync/singleflight"
)

// Cache defines a minimal interface for storing range responses.
type Cache interface {
	Clear()
	Delete(key string)
	Get(key string) (*CachedEntry, bool)
	Put(key string, entry *CachedEntry)
}

// CachedEntry stores the response body and associated validation metadata.
type CachedEntry struct {
	Data []byte
	Meta Metadata
}

// MemoryCache is a simple in-memory implementation.
type MemoryCache struct {
	mu sync.Mutex
	m  map[string]*CachedEntry
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{m: make(map[string]*CachedEntry)}
}

func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = make(map[string]*CachedEntry)
}

func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
}

func (c *MemoryCache) Get(k string) (*CachedEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *MemoryCache) Put(k string, v *CachedEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
}

// CachedRangeTransport caches HTTP Range GET responses and validates via ETag/Last-Modified.
type CachedRangeTransport struct {
	Transport http.RoundTripper
	Cache     Cache
	group     singleflight.Group
}

// RoundTrip implements http.RoundTripper with Range caching and validation.
func (t *CachedRangeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Transport == nil {
		t.Transport = http.DefaultTransport
	}

	// Only cache GET requests with Range headers.
	if req.Method != "GET" {
		return t.Transport.RoundTrip(req)
	}
	rangeHdr := req.Header.Get("Range")
	if rangeHdr == "" {
		return t.Transport.RoundTrip(req)
	}

	key := req.URL.String() + "|" + rangeHdr

	// Try cache first.
	if t.Cache != nil {
		if entry, ok := t.Cache.Get(key); ok {
			resp := &http.Response{
				StatusCode:    http.StatusPartialContent,
				Status:        "206 Partial Content",
				Body:          io.NopCloser(bytes.NewReader(entry.Data)),
				ContentLength: int64(len(entry.Data)),
				Header:        http.Header{"Content-Range": []string{rangeHdr}},
				Request:       req,
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
			}
			return resp, nil
		}
	}

	// Use singleflight to avoid concurrent fetches for the same range.
	v, err, _ := t.group.Do(key, func() (any, error) {
		resp, err := t.Transport.RoundTrip(req)
		if err != nil {
			return nil, err
		}

		switch resp.StatusCode {
		case http.StatusPreconditionFailed:
			if t.Cache != nil {
				t.Cache.Delete(key)
			}
			return nil, fmt.Errorf("rangecache: precondition failed (HTTP 412)")

		case http.StatusPartialContent:
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return nil, err
			}
			newMeta := FromHeaders(resp.Header)

			if t.Cache != nil {
				t.Cache.Put(key, &CachedEntry{Data: body, Meta: newMeta})
			}

			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			return resp, nil

		default:
			return resp, nil
		}
	})

	if err != nil {
		return nil, err
	}
	return v.(*http.Response), nil
}
