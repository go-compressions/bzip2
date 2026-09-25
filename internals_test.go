// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

import (
	"bytes"
	"slices"
	"testing"
)

// TestSortRotationsIsAnOrder checks the transform's foundation directly, against
// rotations built and sorted the obvious way.
//
// The obvious way is O(n^2 log n) and useless for a real block, which is why the
// doubling sort exists -- and exactly why it is the right thing to compare
// against on small inputs.
func TestSortRotationsIsAnOrder(t *testing.T) {
	for _, s := range []string{
		"", "a", "aa", "aaa", "ab", "ba", "banana", "mississippi",
		"aaaaaaaa", "abababab", "the quick brown fox", "\x00\xff\x00\xff",
	} {
		got := sortRotations([]byte(s))
		n := len(s)
		if len(got) != n {
			t.Errorf("%q: %d indices, want %d", s, len(got), n)
			continue
		}
		// Every index once.
		seen := slices.Clone(got)
		slices.Sort(seen)
		for i, v := range seen {
			if int(v) != i {
				t.Errorf("%q: indices are not a permutation: %v", s, got)
				break
			}
		}
		// And in rotation order.
		rot := func(i int32) string { return s[i:] + s[:i] }
		for i := 1; i < n; i++ {
			if rot(got[i-1]) > rot(got[i]) {
				t.Errorf("%q: rotation %d (%q) sorts after %d (%q)",
					s, got[i-1], rot(got[i-1]), got[i], rot(got[i]))
			}
		}
	}
}

// TestTransformIsInvertible. The Burrows-Wheeler transform is only useful
// because it can be undone, and the inverse is the thing every decoder runs. It
// is done here the textbook way, which is not the code under test.
//
// ⛔ The first version of the INVERSE below was off by one rotation -- "banana"
// came back "ananab" -- and it named the transform as the culprit. It was not:
// both real decoders already read this writer's output. A harness can fabricate a
// defect, and the way to tell is which witness is older.
func TestTransformIsInvertible(t *testing.T) {
	for _, s := range []string{"a", "banana", "mississippi", "abababab",
		"the quick brown fox jumps over the lazy dog", "\x00\x01\x02\x00\x01\x02"} {
		out, origPtr := transform([]byte(s))
		if got := string(inverse(out, origPtr)); got != s {
			t.Errorf("%q round-tripped to %q (origPtr %d)", s, got, origPtr)
		}
	}
}

// inverse undoes a Burrows-Wheeler transform, by sorting the column and walking
// the next-index vector. Deliberately the naive textbook form.
func inverse(last []byte, origPtr int) []byte {
	n := len(last)
	if n == 0 {
		return nil
	}
	// first is the sorted column; rank[i] is where last[i] lands in it, counting
	// equal bytes in order.
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return int(last[a]) - int(last[b]) })
	next := make([]int, n)
	for r, i := range idx {
		next[r] = i
	}
	out := make([]byte, 0, n)
	p := origPtr
	for range n {
		p = next[p]
		out = append(out, last[p])
	}
	return out
}

// TestAppendZeroRunMatchesTheDecodersArithmetic.
//
// The run length is in bijective base two, and the decoder rebuilds it as
// sum(place * (1 or 2)) with place doubling. So the check is not a table of
// spellings somebody wrote down: it REPLAYS the decoder's own sum and compares.
func TestAppendZeroRunMatchesTheDecodersArithmetic(t *testing.T) {
	for n := 1; n <= 5000; n++ {
		syms := appendZeroRun(nil, n)
		if len(syms) == 0 {
			t.Fatalf("%d zeros encoded as nothing", n)
		}
		// The decoder: repeat += place << v, place doubling from 1.
		got, place := 0, 1
		for _, s := range syms {
			if s != symRunA && s != symRunB {
				t.Fatalf("%d zeros produced symbol %d, want only RUNA/RUNB", n, s)
			}
			got += place << s
			place <<= 1
		}
		if got != n {
			t.Errorf("%d zeros -> %v, which the decoder reads as %d", n, syms, got)
		}
	}
	if syms := appendZeroRun(nil, 0); syms != nil {
		t.Errorf("a run of no zeros produced %v, want nothing", syms)
	}
}

// TestMoveToFrontEndsWithEndOfBlockAndNeverEmitsOne.
//
// Symbol 1 is RUNB, and move-to-front index 0 is never encoded as a symbol
// because a byte already at the front is always part of a zero run. So index j
// becomes j+1 and nothing collides -- getting that off by one shifts every symbol
// and still decodes, into rubbish.
func TestMoveToFrontEndsWithEndOfBlockAndNeverEmitsOne(t *testing.T) {
	for _, s := range []string{"a", "aaaa", "abc", "banana", "mississippi",
		string(allBytes())} {
		syms, used := moveToFront([]byte(s))
		if len(syms) == 0 {
			t.Fatalf("%q produced no symbols", s)
		}
		eob := uint16(len(used) + 1)
		if got := syms[len(syms)-1]; got != eob {
			t.Errorf("%q ends with %d, want the end-of-block symbol %d", s, got, eob)
		}
		for i, sym := range syms[:len(syms)-1] {
			if sym == eob {
				t.Errorf("%q: end-of-block at position %d of %d", s, i, len(syms))
			}
			if sym > eob {
				t.Errorf("%q: symbol %d is above the alphabet's %d", s, sym, eob)
			}
		}
	}
}

// TestCodeLengthsStayInRangeEvenWhenTheTreeWantsToBeDeep.
//
// Fibonacci frequencies are the worst case for a Huffman tree: each symbol pairs
// with the sum of all the lighter ones, so the tree is a stick and its depth is
// the symbol count. With more than twenty symbols that breaks the format's limit,
// and the weights have to be flattened until it does not.
func TestCodeLengthsStayInRangeEvenWhenTheTreeWantsToBeDeep(t *testing.T) {
	const n = 40
	freq := make([]int32, n)
	a, b := int32(1), int32(1)
	for i := range freq {
		freq[i] = a
		a, b = b, a+b
		if a < 0 || a > 1<<28 {
			a, b = 1, 1 // keep it inside int32 while staying skewed
		}
	}
	lengths := codeLengths(freq, n)
	if len(lengths) != n {
		t.Fatalf("%d lengths, want %d", len(lengths), n)
	}
	for i, l := range lengths {
		if l < 1 || l > maxCodeLen {
			t.Errorf("symbol %d got length %d, outside 1..%d", i, l, maxCodeLen)
		}
	}
	// And the lengths must still be a usable code: Kraft's sum cannot exceed 1.
	kraft := 0.0
	for _, l := range lengths {
		kraft += 1.0 / float64(int64(1)<<l)
	}
	if kraft > 1.0000001 {
		t.Errorf("Kraft sum %.6f > 1: these lengths are not a prefix code", kraft)
	}
}

// TestASingleSymbolAlphabetGetsALengthOfOne. A code of no bits cannot be written
// and the decoder refuses a length of zero, so the degenerate tree has to be
// given a depth rather than measured.
func TestASingleSymbolAlphabetGetsALengthOfOne(t *testing.T) {
	lengths := codeLengths([]int32{5}, 1)
	if len(lengths) != 1 || lengths[0] != 1 {
		t.Errorf("got %v, want [1]", lengths)
	}
}

// TestAssignCodesIsAPrefixCode, checked by the property rather than a table: no
// code is a prefix of another, which is the only thing a decoder needs.
func TestAssignCodesIsAPrefixCode(t *testing.T) {
	for _, lengths := range [][]uint8{
		{1, 2, 2}, {2, 2, 2, 2}, {1, 3, 3, 3, 3}, {3, 3, 2, 2, 2},
		{1, 2, 3, 4, 5, 6, 6},
	} {
		codes := assignCodes(lengths)
		type code struct {
			bits string
		}
		seen := make([]code, 0, len(codes))
		for i, c := range codes {
			l := int(lengths[i])
			var sb bytes.Buffer
			for b := l - 1; b >= 0; b-- {
				if c>>uint(b)&1 != 0 {
					sb.WriteByte('1')
				} else {
					sb.WriteByte('0')
				}
			}
			seen = append(seen, code{sb.String()})
		}
		for i := range seen {
			for j := range seen {
				if i == j {
					continue
				}
				if bytes.HasPrefix([]byte(seen[j].bits), []byte(seen[i].bits)) {
					t.Errorf("lengths %v: %q is a prefix of %q", lengths, seen[i].bits, seen[j].bits)
				}
			}
		}
	}
}
