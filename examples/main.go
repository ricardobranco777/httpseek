/* SPDX-License-Identifier: BSD-2-Clause */

package main

import (
	"fmt"
	"io"

	"github.com/ricardobranco777/httpseek"
)

func main() {
	url := "https://download.freebsd.org/releases/amd64/amd64/ISO-IMAGES/14.3/FreeBSD-14.3-RELEASE-amd64-disc1.iso"

	httpseek.SetLogger(httpseek.StdLogger())

	r, err := httpseek.Open(url)
	if err != nil {
		panic(err)
	}
	defer r.Close()

	buf := make([]byte, 16)
	n, err := r.ReadAt(buf, 512)
	if err != nil && err != io.EOF {
		panic(err)
	}

	fmt.Printf("Read %d bytes from offset 512\n", n)

	// Seek to byte 1024
	r.Seek(512, io.SeekStart)

	// Read another 16 bytes
	n, _ = r.Read(buf)
	fmt.Printf("Bytes [1024:1040): %q\n", buf[:n])

	fmt.Printf("Read %d bytes from offset 512\n", n)
}
