// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

import (
	"bytes"
	stdbzip2 "compress/bzip2"
	"io"
	"testing"
)

// FuzzRoundTrip: whatever the writer is given, in any split, at any level, the
// standard library's decoder (which checks both checksums) reads the input
// back, and the block buffer never moves. The encoder takes arbitrary bytes, so
// this is where a sorter or run-length defect (the first version panicked on
// "qqq") would show.
func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte("qqq"), 1, uint8(0))
	f.Add([]byte("aaaabaaaabaaaab"), 5, uint8(3))
	f.Add(bytes.Repeat([]byte{0}, 1000), 999, uint8(8))
	f.Add([]byte{}, 0, uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, split int, level uint8) {
		lv := 1 + int(level)%9
		split = max(0, min(split, len(data)))
		var out bytes.Buffer
		z, err := NewWriterLevel(&out, lv)
		if err != nil {
			t.Fatal(err)
		}
		z.Write(data[:split])
		z.Write(data[split:])
		if cap(z.block) != lv*100000 {
			t.Fatalf("block buffer moved: cap %d at level %d", cap(z.block), lv)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(stdbzip2.NewReader(&out))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("round trip of %d bytes at level %d: err=%v", len(data), lv, err)
		}
	})
}
