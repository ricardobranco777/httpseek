/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// MmapBlockCache provides an mmap-backed block cache with validity tracking.
// Each block is represented by a bit in the validity bitmap.
type MmapBlockCache struct {
	data      []byte   // mmap-backed data
	blockSize int64
	numBlocks int64
	valid     *Bitset
	mu        sync.RWMutex
}

// NewMmapBlockCache creates a new mmap-backed cache with the given total size and block size.
// It uses an anonymous memory mapping (portable across Unix-like systems).
func NewMmapBlockCache(totalSize, blockSize int64) (*MmapBlockCache, error) {
	if blockSize <= 0 || totalSize <= 0 {
		return nil, fmt.Errorf("invalid sizes: total=%d block=%d", totalSize, blockSize)
	}
	if totalSize%blockSize != 0 {
		return nil, fmt.Errorf("total size must be a multiple of block size")
	}
	numBlocks := totalSize / blockSize

	// Use unix.Mmap for modern, portable memory mapping
	data, err := unix.Mmap(
		-1, 0,
		int(totalSize),
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE,
	)
	if err != nil {
		return nil, os.NewSyscallError("mmap", err)
	}

	return &MmapBlockCache{
		data:      data,
		blockSize: blockSize,
		numBlocks: numBlocks,
		valid:     NewBitset(int(numBlocks)),
	}, nil
}

// Clear invalidates all cached blocks but keeps the mapping.
func (c *MmapBlockCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid = NewBitset(int(c.numBlocks))
	for i := range c.data {
		c.data[i] = 0
	}
}

// Delete invalidates a specific block.
func (c *MmapBlockCache) Delete(block int64) {
	if block < 0 || block >= c.numBlocks {
		// TODO panic
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid.Clear(int(block))
	start := block * c.blockSize
	for i := int64(0); i < c.blockSize; i++ {
		c.data[start+i] = 0
	}
}

// Get returns the block data if valid; otherwise false.
func (c *MmapBlockCache) Get(block int64) ([]byte, bool) {
	if block < 0 || block >= c.numBlocks {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.valid.Get(int(block)) {
		return nil, false
	}
	start := block * c.blockSize
	end := start + c.blockSize
	return c.data[start:end:end], true
}

// Put stores data for a block and marks it as valid.
// If len(data) < blockSize, the remainder is zero-filled.
func (c *MmapBlockCache) Put(block int64, data []byte) {
	if block < 0 || block >= c.numBlocks {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	start := block * c.blockSize
	end := start + c.blockSize
	copy(c.data[start:end], data)
	if int64(len(data)) < c.blockSize {
		for i := start + int64(len(data)); i < end; i++ {
			c.data[i] = 0
		}
	}
	c.valid.Set(int(block))
}

// Close unmaps the cache memory.
func (c *MmapBlockCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		return nil
	}
	err := unix.Munmap(c.data)
	if err != nil {
		return os.NewSyscallError("munmap", err)
	}
	c.data = nil
	return nil
}

// Size returns total mapped size.
func (c *MmapBlockCache) Size() int64 { return int64(len(c.data)) }

// NumBlocks returns number of blocks.
func (c *MmapBlockCache) NumBlocks() int64 { return c.numBlocks }

// BlockSize returns block size.
func (c *MmapBlockCache) BlockSize() int64 { return c.blockSize }
