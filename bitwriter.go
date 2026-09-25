// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

import "io"

// bitWriter writes bits MOST significant first, which is bzip2's order
// throughout: the stream is a bit stream and only the final byte is padded.
//
// Every multi-bit field in this format is big-endian in the same sense -- the
// 24-bit origPtr, the 48-bit block magic, the 32-bit CRCs. Mixing in a
// little-endian field is the mistake that produces a plausible-looking header a
// decoder reads nonsense from.
type bitWriter struct {
	w   io.Writer
	acc uint64 // pending bits, left-aligned at bit 63-n
	n   uint   // how many bits are pending, always < 8 after a flush
	buf []byte
	err error
}

func newBitWriter(w io.Writer) *bitWriter {
	return &bitWriter{w: w, buf: make([]byte, 0, 4096)}
}

// writeBits writes the low n bits of v, most significant first. n <= 56.
func (b *bitWriter) writeBits(n uint, v uint64) {
	if b.err != nil {
		return
	}
	b.acc = b.acc<<n | (v & (1<<n - 1))
	b.n += n
	for b.n >= 8 {
		b.n -= 8
		b.buf = append(b.buf, byte(b.acc>>b.n))
		if len(b.buf) >= 4096 {
			b.drain()
		}
	}
}

// writeZeroBit writes a single clear bit.
//
// There is no writeOneBit: every lone bit this format needs is a zero -- the
// deprecated "randomised" flag, the end of a code-length walk, a selector of
// zero in unary. A writeBit(bool) would carry a branch nothing takes.
func (b *bitWriter) writeZeroBit() {
	b.writeBits(1, 0)
}

// drain hands the whole bytes to the writer. Pending bits stay pending.
func (b *bitWriter) drain() {
	if b.err != nil || len(b.buf) == 0 {
		return
	}
	_, b.err = b.w.Write(b.buf)
	b.buf = b.buf[:0]
}

// close pads the last byte with ZEROS and flushes.
//
// The padding is part of the format rather than a convenience: a bzip2 stream
// ends mid-byte far more often than not, and the reference pads with zeros. A
// decoder never reads them, but a byte-for-byte comparison with the reference
// does.
func (b *bitWriter) close() error {
	if b.n > 0 {
		b.buf = append(b.buf, byte(b.acc<<(8-b.n)))
		b.n = 0
	}
	b.drain()
	return b.err
}
