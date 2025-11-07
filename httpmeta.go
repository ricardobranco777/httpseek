/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import "net/http"

// Metadata captures ETag and Last-Modified headers for cache validation.
type Metadata struct {
	ETag         string
	LastModified string
}

// FromHeaders extracts metadata from response headers.
func FromHeaders(h http.Header) Metadata {
	return Metadata{
		ETag:         h.Get("ETag"),
		LastModified: h.Get("Last-Modified"),
	}
}

// Equal reports whether two metadata values represent the same resource version.
func (m Metadata) Equal(other Metadata) bool {
	if m.ETag != "" && other.ETag != "" && m.ETag != other.ETag {
		return false
	}
	if m.LastModified != "" && other.LastModified != "" && m.LastModified != other.LastModified {
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
