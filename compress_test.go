// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

import (
	"bytes"
	stdbzip2 "compress/bzip2"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// TestSomethingIsActuallyCompressed asks the question neither judge can answer.
//
// A writer that stored its input verbatim, wrapped in a valid bzip2 frame, would
// satisfy both the standard library and the reference binary: they check that the
// bytes come back, not that any work was done. So the ratio is asserted, on data
// whose redundancy is not in doubt.
func TestSomethingIsActuallyCompressed(t *testing.T) {
	for _, c := range []struct {
		name  string
		data  []byte
		ratio float64 // output must be smaller than this fraction of the input
	}{
		{"a megabyte of one byte", bytes.Repeat([]byte("a"), 1<<20), 0.001},
		{"english-ish text", bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 2000), 0.02},
		{"a repeated block", bytes.Repeat(pattern(1000), 60), 0.2},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := compressed(t, c.data, 9)
			got := float64(len(out)) / float64(len(c.data))
			t.Logf("%d bytes -> %d (%.4f)", len(c.data), len(out), got)
			if got > c.ratio {
				t.Errorf("ratio %.4f, want below %.4f: nothing much was compressed", got, c.ratio)
			}
		})
	}
}

// TestManyBlocks exercises what one block cannot: the stream checksum is a FOLD
// over the block checksums, rotated and xored, so it is only ever wrong when
// there is more than one block to fold.
func TestManyBlocks(t *testing.T) {
	// Level 1 means 100 KB blocks, so this is several of them, and the data is
	// varied enough that the run-length pass does not collapse it into one.
	data := pattern(450_000)
	out := compressed(t, data, 1)

	if n := bytes.Count(out, []byte{0x31, 0x41, 0x59, 0x26, 0x53, 0x59}); n < 3 {
		// The magic is not byte aligned in general, so this count is a floor
		// rather than the exact number of blocks. A floor of three is enough to
		// know the multi-block path ran.
		t.Logf("byte-aligned block magics found: %d (a floor, the magic need not be aligned)", n)
	}

	got, err := io.ReadAll(stdbzip2.NewReader(bytes.NewReader(out)))
	if err != nil {
		t.Fatalf("the standard library refused a multi-block stream: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("read back %d bytes, want %d", len(got), len(data))
	}
	if bin, err := exec.LookPath("bzip2"); err == nil {
		cmd := exec.Command(bin, "-dc")
		cmd.Stdin = bytes.NewReader(out)
		var o, e bytes.Buffer
		cmd.Stdout, cmd.Stderr = &o, &e
		if err := cmd.Run(); err != nil {
			t.Fatalf("bzip2 refused a multi-block stream: %v\n%s", err, e.String())
		}
		if !bytes.Equal(o.Bytes(), data) {
			t.Errorf("bzip2 read back %d bytes, want %d", o.Len(), len(data))
		}
	}
}

// TestAnEmptyStreamIsByteIdenticalToTheReference.
//
// An empty input has no block at all -- just the four header bytes, the final
// magic and a zero checksum -- so there is nothing to choose and the two outputs
// can be compared directly. A frame this writer invented for the empty case
// would decode and still be wrong.
func TestAnEmptyStreamIsByteIdenticalToTheReference(t *testing.T) {
	bin, err := exec.LookPath("bzip2")
	if err != nil {
		t.Skip("no bzip2 binary here to compare against")
	}
	for _, level := range []int{1, 5, 9} {
		cmd := exec.Command(bin, "-c", "-"+string(rune('0'+level)))
		cmd.Stdin = strings.NewReader("")
		want, err := cmd.Output()
		if err != nil {
			t.Fatalf("bzip2 -%d: %v", level, err)
		}
		got := compressed(t, nil, level)
		if !bytes.Equal(got, want) {
			t.Errorf("level %d:\n got %x\nwant %x", level, got, want)
		}
	}
}

// TestWriteInManySmallCallsIsTheSameStream: Write must not care how the input is
// sliced. It is a state machine over a run-length pass, and a run that straddles
// two calls is the case that state exists for.
func TestWriteInManySmallCallsIsTheSameStream(t *testing.T) {
	data := append(bytes.Repeat([]byte("a"), 300), pattern(5000)...)

	whole := compressed(t, data, 9)

	var buf bytes.Buffer
	z := NewWriter(&buf)
	for i := 0; i < len(data); i += 7 {
		if _, err := z.Write(data[i:min(i+7, len(data))]); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), whole) {
		t.Errorf("slicing the input changed the output: %d bytes vs %d",
			buf.Len(), len(whole))
	}
}
