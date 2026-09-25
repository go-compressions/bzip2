// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

// The two symbols that encode a run of zeros, and what the rest mean.
//
// After the move-to-front pass most of the output is zeros -- that is the whole
// point of putting MTF after the Burrows-Wheeler transform -- so the zeros are
// run-length encoded and the runs get their own two symbols.
//
//	0          RUNA
//	1          RUNB
//	2..nInUse  move-to-front index 1..nInUse-1
//	nInUse+1   end of block
//
// ⛔ Index 0 is NOT encoded as a symbol. A byte already at the front of the list
// is always part of a zero run, so the symbol for index j is j+1 and nothing
// maps to 1 twice. The decoder's comment says the same thing from its side, and
// getting it wrong shifts every symbol by one -- which decodes, into rubbish.
const (
	symRunA = 0
	symRunB = 1
)

// moveToFront returns the symbol stream for one block, and which byte values it
// used.
func moveToFront(bwt []byte) (syms []uint16, used []byte) {
	var present [256]bool
	for _, b := range bwt {
		present[b] = true
	}
	used = make([]byte, 0, 256)
	for i := range present {
		if present[i] {
			used = append(used, byte(i))
		}
	}
	// The list starts as the used byte values in ascending order, which is what
	// the decoder builds from the symbol map. Anything else and the two lists
	// diverge on the first byte.
	list := make([]byte, len(used))
	copy(list, used)

	syms = make([]uint16, 0, len(bwt)+len(bwt)/8+2)
	zeros := 0
	for _, b := range bwt {
		j := 0
		for list[j] != b {
			j++
		}
		if j == 0 {
			zeros++
			continue
		}
		syms = appendZeroRun(syms, zeros)
		zeros = 0
		copy(list[1:j+1], list[0:j])
		list[0] = b
		syms = append(syms, uint16(j+1))
	}
	syms = appendZeroRun(syms, zeros)
	syms = append(syms, uint16(len(used)+1)) // end of block
	return syms, used
}

// appendZeroRun writes a run of n zeros as RUNA/RUNB.
//
// The encoding is bijective base two: place values are 1, 2, 4, ... and each
// digit is 1 (RUNA) or 2 (RUNB), so every positive integer has exactly one
// representation and none of them is empty. A plain binary encoding would give
// two spellings of some lengths and none of zero, which is why the format uses
// this one.
//
//	1 -> A        3 -> AA       5 -> BA
//	2 -> B        4 -> BA? no: 4 -> B,A is 2+2; see the table in the tests
func appendZeroRun(syms []uint16, n int) []uint16 {
	if n == 0 {
		return syms
	}
	n--
	for {
		if n&1 != 0 {
			syms = append(syms, symRunB)
		} else {
			syms = append(syms, symRunA)
		}
		if n < 2 {
			return syms
		}
		n = (n - 2) / 2
	}
}
