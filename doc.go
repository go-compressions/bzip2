// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

// Package bzip2 implements the bzip2 compressed data format in pure Go, with no
// cgo.
//
// # The half that was missing
//
// Go's standard library has compress/bzip2, and it only DECOMPRESSES. So a
// program that can read a .tar.bz2 cannot write one, and the usual answer is to
// shell out to the bzip2 binary — which is a C program, an external dependency,
// and absent on most of the targets Go cross-compiles to.
//
// This package is the encoder. There is no decoder here on purpose: the standard
// library's is correct, maintained and already everywhere, and a second one would
// be a duplicate whose only distinction is being ours.
//
//	z := bzip2.NewWriter(f)
//	io.Copy(z, src)
//	z.Close()                 // the stream is complete only after this
//
// # What bzip2 does
//
// Four transforms, in this order, and the interesting one is the second:
//
//   - a run-length pass, replacing runs of four or more identical bytes with four
//     of them and a count — which can GROW its input by a quarter, and is why the
//     block size bounds this pass's output rather than the caller's bytes;
//   - the Burrows-Wheeler transform, which sorts every cyclic rotation of the
//     block and keeps the last column. It compresses nothing by itself: it
//     rearranges the block so that bytes with similar contexts end up adjacent;
//   - move-to-front, which turns those adjacencies into small numbers and mostly
//     zeros, with the zero runs given their own two symbols;
//   - Huffman coding.
//
// # Levels
//
// A level is a block SIZE, from 100 KiB at 1 to 900 KiB at 9, not an effort
// setting. bzip2 does the same work either way; a larger block simply gives the
// transform more to find redundancy in. [DefaultLevel] is 9, which is what the
// reference implementation does when asked for nothing in particular.
//
// # How good is it
//
// Smaller than the input, and bigger than the reference: on 106 KB of
// English-like text, bzip2 -9 produces 11 526 bytes and this produces 13 123 —
// about 14% more.
//
// The gap is one thing. bzip2 may use up to six Huffman tables per block and
// spends four passes deciding which group of fifty symbols each table codes best;
// this writer emits the two the format requires as a minimum, identical, with
// every selector choosing the first. Adding the iteration is the obvious next
// step and it changes no bit of the framing around it.
//
// # Verification
//
// Every archive in the tests is read back by two decoders written by other
// people: the standard library's compress/bzip2, and the reference bzip2 binary
// where one exists — including `bzip2 -t`, which checks both checksums rather
// than merely parsing. An empty stream is compared BYTE FOR BYTE with the
// reference's, at three levels, because an empty bzip2 file has no block in it
// and so nothing is left to choose.
package bzip2
