/* SPDX-License-Identifier: BSD-2-Clause */

package httpseek

type Bitset struct {
	bits []uint64
}

func NewBitset(n int) *Bitset {
	return &Bitset{bits: make([]uint64, (n+63)/64)}
}

func (b *Bitset) Set(i int) {
	b.bits[i/64] |= 1 << (i % 64)
}

func (b *Bitset) Clear(i int) {
	b.bits[i/64] &^= 1 << (i % 64)
}

func (b *Bitset) Get(i int) bool {
	return (b.bits[i/64]>>(i%64))&1 != 0
}
