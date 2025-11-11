/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"net/http"
	"strconv"
	"strings"
)

// Metadata captures ETag and Last-Modified headers for cache validation.
type Metadata struct {
	ETag         string
	LastModified string
	Length       int64
}

// extractMetadata extracts metadata from response headers.
// If Etag & Last-Modified are not present, it falls back
// to comparing the original Content-Length with the total
// returned by Content-Range in 206 Partial Content responses.
func extractMetadata(h http.Header) Metadata {
	m := Metadata{
		ETag:         h.Get("ETag"),
		LastModified: h.Get("Last-Modified"),
	}

	// Content-Range (from 206 Partial Content)
	if cr := h.Get("Content-Range"); cr != "" {
		// Format: "bytes start-end/total"
		if parts := strings.Split(cr, "/"); len(parts) == 2 {
			if length, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				m.Length = length
			}
		}
	} else {
		// Fallback: Content-Length (from HEAD or full GET)
		if cl := h.Get("Content-Length"); cl != "" {
			if length, err := strconv.ParseInt(cl, 10, 64); err == nil {
				m.Length = length
			}
		}
	}

	return m
}

// Equal reports whether two metadata values represent the same resource version.
func (m Metadata) Equal(other Metadata) bool {
	if m.ETag != "" && other.ETag != "" && m.ETag != other.ETag {
		return false
	}
	if m.LastModified != "" && other.LastModified != "" && m.LastModified != other.LastModified {
		return false
	}
	if m.Length > 0 && other.Length > 0 && m.Length != other.Length {
		return false
	}
	return true
}

// ApplyValidators adds conditional headers to a request (for conditional GETs).
func (m Metadata) ApplyValidators(h http.Header) {
	if m.ETag != "" {
		h.Set("If-Match", m.ETag)
	}
	if m.LastModified != "" {
		h.Set("If-Unmodified-Since", m.LastModified)
	}
}
