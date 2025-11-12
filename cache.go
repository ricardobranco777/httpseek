/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

// Cache is a minimal key-value interface for storing byte slices.
// Implementations must be safe for concurrent use.
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, data []byte)
}
