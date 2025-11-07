/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"net/http"
	"testing"
)

// helper to build headers
func hdr(kv ...string) http.Header {
	h := make(http.Header)
	for i := 0; i < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestFromHeaders_FullMetadata(t *testing.T) {
	h := hdr(
		"ETag", `"abc123"`,
		"Last-Modified", "Tue, 06 Nov 2025 19:00:00 GMT",
	)

	m := FromHeaders(h)

	if m.ETag != `"abc123"` {
		t.Errorf("expected ETag %q, got %q", `"abc123"`, m.ETag)
	}
	if m.LastModified != "Tue, 06 Nov 2025 19:00:00 GMT" {
		t.Errorf("expected Last-Modified, got %q", m.LastModified)
	}
}

func TestApplyValidators_SetsPreconditionHeaders(t *testing.T) {
	meta := Metadata{
		ETag:         `"xyz"`,
		LastModified: "Wed, 07 Nov 2025 12:00:00 GMT",
	}
	h := make(http.Header)
	meta.ApplyValidators(h)

	if got := h.Get("If-Match"); got != `"xyz"` {
		t.Errorf("expected If-Match %q, got %q", `"xyz"`, got)
	}
	if got := h.Get("If-Unmodified-Since"); got != "Wed, 07 Nov 2025 12:00:00 GMT" {
		t.Errorf("expected If-Unmodified-Since %q, got %q", "Wed, 07 Nov 2025 12:00:00 GMT", got)
	}
}

func TestApplyValidators_EmptyDoesNothing(t *testing.T) {
	h := make(http.Header)
	Metadata{}.ApplyValidators(h)

	if len(h) != 0 {
		t.Errorf("expected no headers set, got %+v", h)
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b Metadata
		want bool
	}{
		{
			name: "equal both empty",
			a:    Metadata{},
			b:    Metadata{},
			want: true,
		},
		{
			name: "equal ETag and Last-Modified",
			a:    Metadata{ETag: "abc", LastModified: "time"},
			b:    Metadata{ETag: "abc", LastModified: "time"},
			want: true,
		},
		{
			name: "different ETag",
			a:    Metadata{ETag: "a"},
			b:    Metadata{ETag: "b"},
			want: false,
		},
		{
			name: "different Last-Modified",
			a:    Metadata{LastModified: "t1"},
			b:    Metadata{LastModified: "t2"},
			want: false,
		},
		{
			name: "one empty, one not (permissive match)",
			a:    Metadata{ETag: "x"},
			b:    Metadata{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Equal(tt.b)
			if got != tt.want {
				t.Errorf("Equal() = %v, want %v (a=%+v, b=%+v)", got, tt.want, tt.a, tt.b)
			}
		})
	}
}
