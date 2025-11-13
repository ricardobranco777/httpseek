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
	Addr     []byte // mmap’d region
	PageSize int
	done     chan struct{}
}

// Ensure interface sanity
var (
	_ io.Closer = (*UffdHTTPReader)(nil)
)

// NewUffdHTTPReader maps a remote HTTP file using userfaultfd.
func NewUffdHTTPReader(f *HTTPFile) (*UffdHTTPReader, error) {
	pageSize := unix.Getpagesize()

	n := int(f.Size())
	if n <= 0 {
		return nil, fmt.Errorf("invalid size: %d", n)
	}

	length := (n + pageSize - 1) &^ (pageSize - 1)

	// Allocate PROT_NONE region so every access faults.
	addr, err := unix.Mmap(-1, 0, length, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		return nil, fmt.Errorf("mmap failed: %w", err)
	}

	// Create the UFFD instance.
	u, err := uffd.New(uffd.UFFD_USER_MODE_ONLY, 0)
	if err != nil {
		unix.Munmap(addr)
		return nil, fmt.Errorf("userfaultfd: %w", err)
	}

	r := &UffdHTTPReader{
		File:     f,
		Uffd:     u,
		Addr:     addr,
		PageSize: pageSize,
		done:     make(chan struct{}),
	}

	// Register the region for page-fault handling.
	_, err = u.Register(
		uintptr(unsafe.Pointer(&addr[0])),
		uintptr(length),
		uffd.UFFDIO_REGISTER_MODE_MISSING,
	)
	if err != nil {
		u.Close()
		unix.Munmap(addr)
		return nil, fmt.Errorf("userfaultfd register: %w", err)
	}

	go r.faultLoop()

	return r, nil
}

// faultLoop runs in a goroutine and handles all page faults.
func (r *UffdHTTPReader) faultLoop() {
	base := uintptr(unsafe.Pointer(&r.Addr[0]))

	for {
		msg, err := r.Uffd.ReadMsg()
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				log.Printf("uffd read event error: %v", err)
				continue
			}
		}

		switch msg.Event {
		case uffd.UFFD_EVENT_PAGEFAULT:
			fault := (*uffd.UffdMsgPagefault)(unsafe.Pointer(&msg.Data))
			addr := uintptr(fault.Address)
			offset := int64(addr - base)

			// Align to page
			pageOffset := offset &^ int64(r.PageSize-1)

			buf := make([]byte, r.PageSize)
			_, err := r.File.ReadAt(buf, pageOffset)
			if err != nil && !errors.Is(err, io.EOF) {
				log.Fatalf("httpseek: HTTP read failed: %v", err)
			}

			pageAddr := addr &^ uintptr(r.PageSize-1)
			// Copy resolved data into the page
			_, err = r.Uffd.Copy(pageAddr, uintptr(unsafe.Pointer(&buf[0])), uintptr(r.PageSize), uint64(0))
			if err != nil {
				log.Fatalf("uffd.Copy failed: %v", err)
			}

		default:
			log.Printf("uffd: unexpected event type %T", msg)
		}
	}
}

// Close unregisters the UFFD handler and unmaps memory.
func (r *UffdHTTPReader) Close() error {
	close(r.done)
	r.Uffd.Close()
	return unix.Munmap(r.Addr)
}

// Bytes returns the mmap’d region. Accessing it triggers HTTP traffic lazily.
func (r *UffdHTTPReader) Bytes() []byte {
	return r.Addr
}
