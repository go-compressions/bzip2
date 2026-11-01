// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

import (
	"bytes"
	stdbzip2 "compress/bzip2"
	"io"
	"math/rand"
	"testing"
)

// adversarial returns inputs chosen to hurt a block sorter and the run-length
// pass: long runs, tiny periods, a period of half the block, and runs of exactly
// four (which GROW by a quarter and are what the 19-byte slack is for).
func adversarial(n int) map[string][]byte {
	r := rand.New(rand.NewSource(7))
	rnd := make([]byte, n)
	r.Read(rnd)
	zeros := make([]byte, n)
	period := func(p int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i % p)
		}
		return b
	}
	half := make([]byte, n)
	r.Read(half[:n/2])
	copy(half[n/2:], half[:n/2])
	fours := bytes.Repeat([]byte("aaaab"), n/5+1)[:n]
	return map[string][]byte{
		"random": rnd, "zeros": zeros, "period 2": period(2), "period 3": period(3),
		"two equal halves": half, "runs of four": fours,
	}
}

// TestWriter_MemoryIsFixedByTheLevel: the only input-sized state is the block,
// allocated once at level*100000 bytes. The run-length pass emits up to five
// bytes at a time, and the 19-byte slack is what keeps an append from ever
// growing it: if it did, cap would move. Whatever the input does, the sorter's
// arrays are sized by the block (n int32s each), not by what was written.
func TestWriter_MemoryIsFixedByTheLevel(t *testing.T) {
	levels := []int{1, 9}
	if testing.Short() {
		levels = []int{1}
	}
	for _, level := range levels {
		size := level * 100000
		// One and a half blocks: the second block starts mid-call.
		for name, data := range adversarial(size + size/2 + 777) {
			var out bytes.Buffer
			z, err := NewWriterLevel(&out, level)
			if err != nil {
				t.Fatal(err)
			}
			for off := 0; off < len(data); off += 70001 {
				end := min(off+70001, len(data))
				if _, err := z.Write(data[off:end]); err != nil {
					t.Fatal(err)
				}
				if cap(z.block) != size || len(z.block) >= size {
					t.Fatalf("level %d %s: block len=%d cap=%d, want cap %d", level, name, len(z.block), cap(z.block), size)
				}
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(stdbzip2.NewReader(&out))
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("level %d %s: round trip: err=%v len=%d want %d", level, name, err, len(got), len(data))
			}
		}
	}
}

// TestWriter_RefusesALevelBeforeAllocatingFromIt: the level is the only number
// that sizes the writer, and it is checked first.
func TestWriter_RefusesALevelBeforeAllocatingFromIt(t *testing.T) {
	for _, level := range []int{0, -1, 10, 1 << 30} {
		if z, err := NewWriterLevel(io.Discard, level); err == nil || z != nil {
			t.Errorf("level %d accepted", level)
		}
	}
}
