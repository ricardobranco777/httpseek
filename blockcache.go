/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	"golang.org/x/sync/singleflight"
)

// BlockCache defines a minimal interface for storing block responses.
type BlockCache interface {
	Clear()
	Delete(block int64)
	Get(block int64) ([]byte, bool)
	Put(block int64, data []byte)
}

// MemoryBlockCache is a simple in-memory implementation.
type MemoryBlockCache struct {
	mu sync.Mutex
	m  map[int64][]byte
}

func NewMemoryBlockCache() *MemoryBlockCache {
	return &MemoryBlockCache{m: make(map[int64][]byte)}
}

func (c *MemoryBlockCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = make(map[int64][]byte)
}

func (c *MemoryBlockCache) Delete(block int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, block)
}

func (c *MemoryBlockCache) Get(block int64) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[block]
	return v, ok
}

func (c *MemoryBlockCache) Put(block int64, v []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[block] = v
}

// CachedBlockTransport caches aligned Range GET responses.
// It transparently rounds incoming Range headers to fixed-size blocks.
// Each block is stored once and reused for any overlapping request.
type CachedBlockTransport struct {
	Transport http.RoundTripper
	Cache     BlockCache
	BlockSize int64
	group     singleflight.Group
}

// Compile-time check
var _ http.RoundTripper = (*CachedBlockTransport)(nil)

// DefaultBlockSize is the default block alignment size.
const DefaultBlockSize = 512

// RoundTrip implements http.RoundTripper with transparent block-aligned caching.
func (t *CachedBlockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Transport == nil {
		t.Transport = http.DefaultTransport
	}
	if t.BlockSize <= 0 {
		t.BlockSize = DefaultBlockSize
	}
	bs := t.BlockSize

	if req.Method != http.MethodGet {
		return t.Transport.RoundTrip(req)
	}
	rangeHdr := req.Header.Get("Range")
	if rangeHdr == "" {
		return t.Transport.RoundTrip(req)
	}

	// Parse "Range: bytes=start-end". Support open-ended form "bytes=start-".
	var start, end int64
	n, err := fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
	if err != nil || n < 1 {
		return t.Transport.RoundTrip(req)
	}
	if n == 1 || end < start {
		end = start + bs - 1
	}

	blockStart := (start / bs) * bs
	blockEnd := (end / bs) * bs
	numBlocks := ((blockEnd - blockStart) / bs) + 1

	missing := make([]int64, 0, numBlocks)
	for b := blockStart; b <= blockEnd; b += bs {
		blockNum := b / bs
		if t.Cache == nil {
			missing = append(missing, blockNum)
			continue
		}
		if _, ok := t.Cache.Get(blockNum); !ok {
			missing = append(missing, blockNum)
		}
	}

	// Fetch all missing blocks in one contiguous request if needed
	if len(missing) > 0 {
		firstBlock := missing[0]
		lastBlock := missing[len(missing)-1]
		key := strconv.FormatInt(firstBlock, 10)

		_, err, _ = t.group.Do(key, func() (any, error) {
			rangeStart := firstBlock * bs
			rangeEnd := (lastBlock+1)*bs - 1

			newReq := req.Clone(req.Context())
			newReq.Header = req.Header.Clone()
			newReq.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd))
			logRequest(newReq)

			resp, err := t.Transport.RoundTrip(newReq)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			logResponse(resp)

			if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}

			// Split and populate cache
			for i, b := range missing {
				offset := int64(i) * bs
				if offset >= int64(len(body)) {
					break
				}
				end := offset + bs
				if end > int64(len(body)) {
					end = int64(len(body))
				}
				if t.Cache != nil {
					t.Cache.Put(b, body[offset:end])
				}
			}
			return nil, nil
		})
		if err != nil {
			return nil, err
		}
	}

	// Rebuild combined body in logical block order
	combined := make([]byte, 0, int(numBlocks*bs))
	for b := blockStart; b <= blockEnd; b += bs {
		blockNum := b / bs
		if t.Cache != nil {
			if data, ok := t.Cache.Get(blockNum); ok {
				combined = append(combined, data...)
			}
		}
	}

	// Slice to requested sub-range
	offset := start - blockStart
	length := end - start + 1
	if offset < 0 {
		offset = 0
	}
	if offset+length > int64(len(combined)) {
		length = int64(len(combined)) - offset
	}
	if length < 0 {
		length = 0
	}
	data := combined[offset : offset+length]

	resp := &http.Response{
		StatusCode:    http.StatusPartialContent,
		Status:        "206 Partial Content",
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: int64(len(data)),
		Header: http.Header{
			"Content-Range": []string{fmt.Sprintf("bytes %d-%d/*", start, end)},
		},
		Request:    req,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
	return resp, nil
}
