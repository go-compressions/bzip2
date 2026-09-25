// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

// sortRotations returns the indices of the cyclic rotations of s in
// lexicographic order.
//
// It sorts CYCLIC SHIFTS rather than suffixes, by prefix doubling with a
// counting sort at each round: after round h every rotation is ordered by its
// first 2^h bytes, so ceil(log2 n) rounds order them completely. That is
// O(n log n) with a small constant, and it needs no sentinel byte and no doubled
// copy of the input -- both of which are ways of turning a rotation sort into a
// suffix sort and then having to undo the difference.
//
// Rotations that are EQUAL may come out in any order. That is not a defect: if
// two rotations are the same string then their last bytes are the same, so the
// transformed column is identical either way, and the inverse transform works
// from any of their positions.
func sortRotations(s []byte) []int32 {
	n := len(s)
	if n == 0 {
		return nil
	}
	order := make([]int32, n)
	class := make([]int32, n)

	// Round zero: order by the first byte alone, with a counting sort over the
	// 256 values rather than a comparison sort.
	var cnt [256]int32
	for _, b := range s {
		cnt[b]++
	}
	sum := int32(0)
	for i := range cnt {
		c := cnt[i]
		cnt[i] = sum
		sum += c
	}
	for i, b := range s {
		order[cnt[b]] = int32(i)
		cnt[b]++
	}
	classes := int32(1)
	class[order[0]] = 0
	for i := 1; i < n; i++ {
		if s[order[i]] != s[order[i-1]] {
			classes++
		}
		class[order[i]] = classes - 1
	}

	newOrder := make([]int32, n)
	newClass := make([]int32, n)
	bucket := make([]int32, n+1)
	for h := int32(1); classes < int32(n); h <<= 1 {
		// Shift every rotation left by h: a rotation ordered by its first h
		// bytes, paired with the one h further on, is ordered by 2h bytes.
		//
		// ⛔ The wrap is MODULAR, not one addition of n. h doubles past n on any
		// input whose rotations are not all distinct -- "qqq" is enough -- and
		// then order[i]-h is below -n, so adding n once leaves it negative and
		// the class lookup indexes at -1. The first version did exactly that and
		// panicked on three identical bytes.
		shift := h % int32(n)
		for i := range order {
			v := order[i] - shift
			if v < 0 {
				v += int32(n)
			}
			newOrder[i] = v
		}
		// Stable counting sort by the first half's class, which leaves the
		// second half's order -- already sorted -- as the tiebreak.
		b := bucket[:classes+1]
		clear(b)
		for _, v := range newOrder {
			b[class[v]]++
		}
		sum := int32(0)
		for i := range b {
			c := b[i]
			b[i] = sum
			sum += c
		}
		for _, v := range newOrder {
			order[b[class[v]]] = v
			b[class[v]]++
		}
		// Re-class by the PAIR of halves.
		newClass[order[0]] = 0
		next := int32(1)
		for i := 1; i < n; i++ {
			cur, prev := order[i], order[i-1]
			curSecond, prevSecond := cur+h, prev+h
			if curSecond >= int32(n) {
				curSecond -= int32(n)
			}
			if prevSecond >= int32(n) {
				prevSecond -= int32(n)
			}
			if class[cur] != class[prev] || class[curSecond] != class[prevSecond] {
				next++
			}
			newClass[order[i]] = next - 1
		}
		class, newClass = newClass, class
		// Classes that did not grow will never grow: the rotations still sharing
		// one are EQUAL, and doubling the prefix cannot separate equal strings.
		// This is the loop's real exit for a periodic input, where classes never
		// reaches n -- "qqq" has one class for all three rotations, forever.
		if next == classes {
			break
		}
		classes = next
	}
	return order
}

// transform returns the Burrows-Wheeler transform of s and the row its
// unrotated form landed on.
//
// The transformed bytes are the LAST byte of each sorted rotation, which for the
// rotation starting at p is the byte before p.
func transform(s []byte) (out []byte, origPtr int) {
	n := len(s)
	order := sortRotations(s)
	out = make([]byte, n)
	for i, p := range order {
		if p == 0 {
			origPtr = i
			out[i] = s[n-1]
			continue
		}
		out[i] = s[p-1]
	}
	return out, origPtr
}
