// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

import "slices"

// maxCodeLen is the longest code bzip2 allows. The decoder rejects 0 and
// anything above 20, so EVERY symbol needs a length in 1..20 -- including the
// ones that never occur.
const maxCodeLen = 20

// codeLengths assigns a code length to every symbol of the alphabet.
//
// Unused symbols get a length too, because the header carries one per symbol and
// the decoder refuses a zero. They are given the smallest weight rather than
// omitted, which is what the reference does: weight = freq*256 + 1, so a symbol
// that never occurs still joins the tree and lands on a long code.
//
// If the tree comes out deeper than maxCodeLen the weights are halved and it is
// built again. That only happens for frequencies spread like the Fibonacci
// numbers, and halving flattens the tree without changing which symbols are
// common.
func codeLengths(freq []int32, alphaSize int) []uint8 {
	weight := make([]int64, alphaSize)
	for i := range weight {
		weight[i] = int64(freq[i])<<8 + 1
	}
	lengths := make([]uint8, alphaSize)
	for {
		if buildLengths(weight, lengths) {
			return lengths
		}
		for i := range weight {
			weight[i] = (1 + weight[i]>>8/2) << 8
		}
	}
}

// node is one entry of the tree being built.
type node struct {
	weight int64
	left   int32 // -1 for a leaf
	right  int32
}

// buildLengths builds a Huffman tree over weight and writes each leaf's depth
// into lengths. It reports whether every depth fits in maxCodeLen.
//
// A single-symbol alphabet gets length 1 rather than 0: a code of no bits cannot
// be written, and the decoder refuses a length of zero.
func buildLengths(weight []int64, lengths []uint8) bool {
	n := len(weight)
	nodes := make([]node, 0, 2*n)
	live := make([]int32, 0, n)
	for i := range weight {
		nodes = append(nodes, node{weight: weight[i], left: -1, right: -1})
		live = append(live, int32(i))
	}
	// Repeatedly join the two lightest. A heap would be faster; the alphabet
	// here is at most 258 symbols and this runs once per block, so the simple
	// version is the one that is obviously right.
	for len(live) > 1 {
		slices.SortFunc(live, func(a, b int32) int {
			if nodes[a].weight != nodes[b].weight {
				if nodes[a].weight < nodes[b].weight {
					return -1
				}
				return 1
			}
			return int(a - b)
		})
		a, b := live[0], live[1]
		nodes = append(nodes, node{weight: nodes[a].weight + nodes[b].weight, left: a, right: b})
		live = append(live[2:], int32(len(nodes)-1))
	}

	ok := true
	var walk func(i int32, depth int)
	walk = func(i int32, depth int) {
		nd := nodes[i]
		if nd.left < 0 {
			if depth == 0 {
				depth = 1 // the whole alphabet is one symbol
			}
			if depth > maxCodeLen {
				ok = false
				depth = maxCodeLen
			}
			lengths[i] = uint8(depth)
			return
		}
		walk(nd.left, depth+1)
		walk(nd.right, depth+1)
	}
	walk(int32(len(nodes)-1), 0)
	return ok
}

// assignCodes returns the canonical code for every symbol.
//
// Canonical here means: shortest length first, and within one length the lower
// symbol gets the lower code. MEASURED against the standard library's decoder
// rather than assumed -- a first attempt read that decoder's tree as expecting
// the complement of this, because the probe assumed a left-is-zero convention
// while Decode does `if bit == 1 { left }`. Reading how the consumer consumes
// settled it.
func assignCodes(lengths []uint8) []uint32 {
	codes := make([]uint32, len(lengths))
	minLen, maxLen := uint8(maxCodeLen), uint8(1)
	for _, l := range lengths {
		minLen = min(minLen, l)
		maxLen = max(maxLen, l)
	}
	vec := uint32(0)
	for n := minLen; n <= maxLen; n++ {
		for i, l := range lengths {
			if l == n {
				codes[i] = vec
				vec++
			}
		}
		vec <<= 1
	}
	return codes
}
