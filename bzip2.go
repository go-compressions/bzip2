// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

import (
	"errors"
	"fmt"
	"io"
)

// The two magic numbers that delimit the blocks, 48 bits each. They are the
// decimal digits of pi and of sqrt(pi), which is the kind of thing that makes a
// format recognisable and is worth saying out loud because neither looks like a
// constant otherwise.
const (
	blockMagic = 0x314159265359 // 3.141592653589793
	finalMagic = 0x177245385090 // 1.772453850905516
)

// DefaultLevel is the block size the reference implementation uses when asked
// for nothing in particular: 900 KiB, its largest.
const DefaultLevel = 9

// ErrClosed is returned by a Writer that has already been closed.
var ErrClosed = errors.New("bzip2: writer is closed")

// A Writer compresses to the bzip2 format.
//
// Write buffers a block's worth of input and compresses it when full, so the
// output appears in bursts; Close finishes the last block and writes the
// stream's trailer, and the stream is incomplete until it returns.
type Writer struct {
	bw    *bitWriter
	limit int // how many run-length-encoded bytes go in a block
	level int

	// block holds the current block AFTER the first run-length pass, which is
	// the length the format's block size actually bounds.
	block []byte
	// last and runLen are that pass's state: bytes are held back until a run
	// ends, because only then is its length known.
	last   byte
	runLen int

	blockCRC  uint32
	streamCRC uint32
	wroteAny  bool
	closed    bool
}

// NewWriter returns a Writer at DefaultLevel.
func NewWriter(w io.Writer) *Writer { return newWriter(w, DefaultLevel) }

// NewWriterLevel returns a Writer whose blocks hold level*100 KiB, for level in
// 1..9.
//
// The level is a block SIZE, not an effort setting: bzip2 does the same work
// either way and a larger block simply gives the transform more to find
// redundancy in. Nothing is faster at level 1 except the sort.
func NewWriterLevel(w io.Writer, level int) (*Writer, error) {
	if level < 1 || level > 9 {
		return nil, fmt.Errorf("bzip2: level %d out of range 1..9", level)
	}
	return newWriter(w, level), nil
}

// newWriter is NewWriterLevel with the range already checked, so NewWriter needs
// no error path for a level it chose itself.
func newWriter(w io.Writer, level int) *Writer {
	z := &Writer{
		bw: newBitWriter(w),
		// The reference keeps 19 bytes of slack, because the run-length pass
		// emits up to five bytes at once and the block must not be entered with
		// room for fewer.
		limit: level*100000 - 19,
		level: level,
	}
	z.block = make([]byte, 0, level*100000)
	z.bw.writeBits(8, 'B')
	z.bw.writeBits(8, 'Z')
	z.bw.writeBits(8, 'h')
	z.bw.writeBits(8, uint64('0'+level))
	return z
}

// Write compresses p, emitting a block whenever one fills.
func (z *Writer) Write(p []byte) (int, error) {
	if z.closed {
		return 0, ErrClosed
	}
	for _, b := range p {
		z.addByte(b)
		if len(z.block) >= z.limit {
			if err := z.flushBlock(); err != nil {
				return 0, err
			}
		}
	}
	return len(p), z.bw.err
}

// addByte feeds one byte through the first run-length pass.
//
// ⛔ Runs of four or more become four bytes plus a COUNT, and a run of exactly
// four becomes five bytes -- so this pass can GROW its input by a quarter. That
// is why the block limit is measured here and not on the bytes handed to Write.
func (z *Writer) addByte(b byte) {
	if z.runLen > 0 && b == z.last && z.runLen < 259 {
		z.runLen++
		return
	}
	z.emitRun()
	z.last, z.runLen = b, 1
}

// emitRun writes the pending run out, and folds its ORIGINAL bytes into the
// block's checksum.
//
// ⛔ Here, and not in Write. The checksum is of the original bytes of THIS block,
// and a block can close in the middle of a call to Write -- so folding the whole
// of p up front credits the next block's bytes to this one. That is what the
// first version did: every single-block test passed, because with one block the
// two are the same bytes, and the standard library said "block checksum mismatch"
// the moment a second block existed.
//
// This is also the only moment that is right: a run is held back until it ends,
// so until emitRun the bytes are not part of any block yet.
func (z *Writer) emitRun() {
	switch {
	case z.runLen == 0:
		return
	case z.runLen < 4:
		for range z.runLen {
			z.block = append(z.block, z.last)
		}
	default:
		z.block = append(z.block, z.last, z.last, z.last, z.last, byte(z.runLen-4))
	}
	var raw [259]byte
	for i := range z.runLen {
		raw[i] = z.last
	}
	z.blockCRC = updateCRC(z.blockCRC, raw[:z.runLen])
	z.runLen = 0
}

// Close finishes the stream. The output is a complete bzip2 file when it
// returns, and not before.
func (z *Writer) Close() error {
	if z.closed {
		return ErrClosed
	}
	z.emitRun()
	if len(z.block) > 0 {
		if err := z.flushBlock(); err != nil {
			return err
		}
	}
	z.bw.writeBits(48, finalMagic)
	z.bw.writeBits(32, uint64(z.streamCRC))
	z.closed = true
	return z.bw.close()
}

// flushBlock compresses what has accumulated and starts a new block.
//
// Both callers check that there is something to flush -- Write when the block
// reaches its limit, Close when anything is left -- so there is no guard here for
// an empty block. A block of nothing would be a valid-looking frame the decoder
// refuses ("no symbols in input"), which is a good reason for the invariant to
// live at the call sites where it can be read.
func (z *Writer) flushBlock() error {
	z.streamCRC = combineCRC(z.streamCRC, z.blockCRC)
	z.writeBlock(z.block, z.blockCRC)
	z.block = z.block[:0]
	z.blockCRC = 0
	z.wroteAny = true
	return z.bw.err
}

// writeBlock emits one complete block: its header, its Huffman tables and its
// symbols.
func (z *Writer) writeBlock(data []byte, crc uint32) {
	bwt, origPtr := transform(data)
	syms, used := moveToFront(bwt)
	alphaSize := len(used) + 2

	freq := make([]int32, alphaSize)
	for _, s := range syms {
		freq[s]++
	}
	lengths := codeLengths(freq, alphaSize)
	codes := assignCodes(lengths)

	z.bw.writeBits(48, blockMagic)
	z.bw.writeBits(32, uint64(crc))
	z.bw.writeZeroBit() // randomised: deprecated, and the decoder refuses a 1
	z.bw.writeBits(24, uint64(origPtr))
	z.writeSymbolMap(used)

	// TWO tables, identical, and every selector choosing the first.
	//
	// The format requires between two and six, so two is the fewest that is
	// legal. The reference spends four passes assigning groups of fifty symbols
	// to whichever of six tables codes them best, which is where a good part of
	// bzip2's ratio comes from; this writer does not, yet. What it produces is a
	// correct archive that every decoder reads, which is the thing to have first.
	const numTables = 2
	z.bw.writeBits(3, numTables)
	numSelectors := (len(syms) + 49) / 50
	z.bw.writeBits(15, uint64(numSelectors))
	for range numSelectors {
		// Move-to-front of an all-zero list is all zeros, and zero in unary is a
		// single clear bit.
		z.bw.writeZeroBit()
	}
	for range numTables {
		z.writeLengths(lengths)
	}
	for _, s := range syms {
		z.bw.writeBits(uint(lengths[s]), uint64(codes[s]))
	}
}

// writeSymbolMap says which byte values the block uses, as a bitmap of bitmaps:
// sixteen bits for which groups of sixteen appear, then sixteen bits for each
// group that does.
//
// Both levels are most-significant-bit first, so value 0 is bit 15 of the first
// word. Filling from the low bit produces a map of the right LENGTH naming the
// wrong bytes, which decodes into a block of the wrong alphabet.
func (z *Writer) writeSymbolMap(used []byte) {
	var present [256]bool
	for _, b := range used {
		present[b] = true
	}
	var ranges [16]bool
	for i, p := range present {
		if p {
			ranges[i/16] = true
		}
	}
	var top uint64
	for i, r := range ranges {
		if r {
			top |= 1 << (15 - i)
		}
	}
	z.bw.writeBits(16, top)
	for i, r := range ranges {
		if !r {
			continue
		}
		var bits uint64
		for j := range 16 {
			if present[i*16+j] {
				bits |= 1 << (15 - j)
			}
		}
		z.bw.writeBits(16, bits)
	}
}

// writeLengths emits one table's code lengths, delta encoded.
//
// A five-bit starting value, then for each symbol a walk to its length: a set
// bit says another step follows, and the step's own bit says down or up. The
// walk is relative to the PREVIOUS symbol's length, so an absolute value per
// symbol would be a different encoding that happens to start the same way.
func (z *Writer) writeLengths(lengths []uint8) {
	cur := int(lengths[0])
	z.bw.writeBits(5, uint64(cur))
	for _, l := range lengths {
		want := int(l)
		for cur < want {
			z.bw.writeBits(2, 0b10) // another step, upwards
			cur++
		}
		for cur > want {
			z.bw.writeBits(2, 0b11) // another step, downwards
			cur--
		}
		z.bw.writeZeroBit() // no more steps for this symbol
	}
}
