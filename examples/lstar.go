// SPDX-License-Identifier: BSD-2-Clause

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/ricardobranco777/httpseek"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 1 {
		log.Fatalf("usage: %s URL.tar[.gz]\n", os.Args[0])
	}

	// httpseek.SetLogger(httpseek.StdLogger())

	url := os.Args[1]
	f, err := httpseek.Open(url)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer f.Close()

	// Sniff first two bytes to detect gzip (1f 8b)
	var sig [2]byte
	if _, err := f.ReadAt(sig[:], 0); err != nil && err != io.EOF {
		log.Fatalf("sniff: %v", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		log.Fatalf("seek: %v", err)
	}

	var r io.Reader = f
	if sig[0] == 0x1f && sig[1] == 0x8b {
		gz, err := gzip.NewReader(f)
		if err != nil {
			log.Fatalf("gzip: %v", err)
		}
		defer gz.Close()
		r = gz
	}

	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("tar read: %v", err)
		}

		fmt.Printf("%c %9d  %s\n", typeChar(hdr.Typeflag), hdr.Size, hdr.Name)
	}
}

func typeChar(tf byte) byte {
	switch tf {
	case tar.TypeReg:
		return '-'
	case tar.TypeDir:
		return 'd'
	case tar.TypeSymlink:
		return 'l'
	case tar.TypeLink:
		return 'h'
	case tar.TypeChar:
		return 'c'
	case tar.TypeBlock:
		return 'b'
	case tar.TypeFifo:
		return 'p'
	default:
		return '?'
	}
}
