// SPDX-License-Identifier: BSD-2-Clause
package httpseek

import (
	"errors"
	"fmt"
	"io"
	"log"
	"unsafe"

	uffd "github.com/ricardobranco777/go-userfaultfd"
	"golang.org/x/sys/unix"
)

// UffdHTTPReader maps a remote HTTP file into memory and faults pages on demand.
type UffdHTTPReader struct {
	File     *HTTPFile
	Uffd     *uffd.Uffd
	full     []byte // full mmap'd region (page-aligned length)
	data     []byte // user-visible slice: len == file size
	PageSize int

	base   uintptr // start address of mapping
	mapLen int     // page-aligned mapping length
	pos    int64   // read offset for io.Reader

	done chan struct{}
}

// Ensure interface sanity
var (
	_ io.Closer = (*UffdHTTPReader)(nil)
	_ io.Reader = (*UffdHTTPReader)(nil)
)

// roundUp rounds n up to a multiple of align (align must be power of 2).
func roundUp(n, align int) int {
	return (n + align - 1) &^ (align - 1)
}

// NewUffdHTTPReader maps a remote HTTP file using userfaultfd.
// It returns a reader that implements io.Reader and exposes Bytes() for zero-copy access.
func NewUffdHTTPReader(f *HTTPFile) (*UffdHTTPReader, error) {
	pageSize := unix.Getpagesize()

	n := int(f.Size())
	if n <= 0 {
		return nil, fmt.Errorf("invalid size: %d", n)
	}

	// Page-align the mapping length.
	mapLen := roundUp(n, pageSize)

	// Anonymous mapping: pages are missing initially and will fault on first use.
	full, err := unix.Mmap(
		-1,
		0,
		mapLen,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_PRIVATE|unix.MAP_ANONYMOUS,
	)
	if err != nil {
		return nil, fmt.Errorf("mmap failed: %w", err)
	}

	base := uintptr(unsafe.Pointer(&full[0]))

	// Choose flags for userfaultfd.
	flags := 0
	if !uffd.UnprivilegedUserfaultfd && uffd.HaveUserModeOnly {
		flags |= uffd.UFFD_USER_MODE_ONLY
	}

	u, err := uffd.New(flags, 0)
	if err != nil {
		_ = unix.Munmap(full)
		return nil, fmt.Errorf("userfaultfd: %w", err)
	}

	r := &UffdHTTPReader{
		File:     f,
		Uffd:     u,
		full:     full,
		data:     full[:n], // visible file content slice
		PageSize: pageSize,
		base:     base,
		mapLen:   mapLen,
		pos:      0,
		done:     make(chan struct{}),
	}

	// Register the full page-aligned region.
	if _, err = u.Register(base, uintptr(mapLen), uffd.UFFDIO_REGISTER_MODE_MISSING); err != nil {
		_ = u.Close()
		_ = unix.Munmap(full)
		return nil, fmt.Errorf("userfaultfd register: %w", err)
	}

	go r.faultLoop()

	return r, nil
}

// faultLoop runs in a goroutine and handles all page faults.
func (r *UffdHTTPReader) faultLoop() {
	for {
		msg, err := r.Uffd.ReadMsg()
		if err != nil {
			// If we're closing, exit quietly.
			select {
			case <-r.done:
				return
			default:
				log.Printf("httpseek: uffd.ReadMsg error: %v", err)
				continue
			}
		}

		switch msg.Event {
		case uffd.UFFD_EVENT_PAGEFAULT:
			pf := msg.GetPagefault()
			r.handlePageFault(pf)
		default:
			log.Printf("httpseek: unexpected uffd event 0x%x", msg.Event)
		}
	}
}

func (r *UffdHTTPReader) handlePageFault(pf *uffd.UffdMsgPagefault) {
	faultAddr := uintptr(pf.Address)

	// Align to page boundary.
	pageAddr := faultAddr &^ uintptr(r.PageSize-1)

	// Compute offset of this page relative to the base of the mapping.
	offset := int64(pageAddr - r.base)
	if offset < 0 || offset >= int64(r.mapLen) {
		log.Printf("httpseek: page fault out of range: addr=0x%x offset=%d", faultAddr, offset)
		return
	}

	buf := make([]byte, r.PageSize)

	// Compute how much of this page is within the file.
	fileSize := r.File.Size()
	if offset >= fileSize {
		// Entire page is beyond EOF: just zero-fill and install it.
	} else {
		toRead := int64(r.PageSize)
		if offset+toRead > fileSize {
			toRead = fileSize - offset
		}

		n, err := r.File.ReadAt(buf[:toRead], offset)
		if err != nil && !errors.Is(err, io.EOF) {
			log.Fatalf("httpseek: HTTP ReadAt failed at offset %d: %v", offset, err)
		}
		_ = n // buf already contains data; remainder is zero.
	}

	// Satisfy the fault using UFFDIO_COPY with a full page.
	_, err := r.Uffd.Copy(
		pageAddr,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(r.PageSize),
		0,
	)
	if err != nil {
		log.Fatalf("httpseek: uffd.Copy failed at addr=0x%x: %v", pageAddr, err)
	}
}

// Read implements io.Reader on top of the mmap'd region.
// Accessing the region transparently triggers userfaultfd page faults.
func (r *UffdHTTPReader) Read(p []byte) (int, error) {
	if r.pos >= int64(len(r.data)) {
		return 0, io.EOF
	}

	n := copy(p, r.data[r.pos:])
	r.pos += int64(n)

	if n < len(p) || r.pos >= int64(len(r.data)) {
		return n, io.EOF
	}
	return n, nil
}

// Bytes returns the memory-mapped region representing the file contents.
// Accessing it directly also triggers UFFD faults on demand.
func (r *UffdHTTPReader) Bytes() []byte {
	return r.data
}

// Close unregisters the UFFD range, closes the fd, and unmaps memory.
func (r *UffdHTTPReader) Close() error {
	// Signal the fault loop to exit.
	close(r.done)

	// Best-effort cleanup.
	_ = r.Uffd.Unregister(r.base, uintptr(r.mapLen))
	_ = r.Uffd.Close()

	return unix.Munmap(r.full)
}
