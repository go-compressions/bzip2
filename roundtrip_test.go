// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

import (
	"bytes"
	stdbzip2 "compress/bzip2"
	"io"
	"os"
	"os/exec"
	"testing"
)

// compressed returns what this package makes of in.
func compressed(t *testing.T, in []byte, level int) []byte {
	t.Helper()
	var buf bytes.Buffer
	z, err := NewWriterLevel(&buf, level)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := z.Write(in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := z.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

// corpus is what every judge is run over. Each case is here because it exercises
// a branch that a single "hello world" would not reach.
var corpus = []struct {
	name string
	data []byte
}{
	{"empty", nil},
	{"one byte", []byte("x")},
	{"two bytes", []byte("xy")},
	// A run of exactly four is the boundary of the first pass: four bytes plus a
	// count of zero, which is the case that makes that pass GROW its input.
	{"run of exactly four", []byte("aaaa")},
	{"run of five", []byte("aaaaa")},
	{"run of 259", bytes.Repeat([]byte("a"), 259)},
	{"run of 260", bytes.Repeat([]byte("a"), 260)},
	{"run of 1000", bytes.Repeat([]byte("a"), 1000)},
	// One byte value in the whole block: the alphabet has a single symbol, which
	// is where a Huffman length of zero would be produced by a naive tree.
	{"single value", bytes.Repeat([]byte("q"), 3)},
	{"two values", []byte("abababababab")},
	{"text", []byte("the quick brown fox jumps over the lazy dog, and then does it again")},
	{"all 256 values", allBytes()},
	{"all 256 values twice", append(allBytes(), allBytes()...)},
	{"zeros", make([]byte, 5000)},
	{"binary", pattern(40000)},
}

func allBytes() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// pattern is deterministic pseudo-random data: compressible enough to exercise
// the coder, varied enough to use the whole alphabet.
func pattern(n int) []byte {
	b := make([]byte, n)
	x := uint32(1)
	for i := range b {
		x = x*1664525 + 1013904223
		b[i] = byte(x >> 24)
		if i%7 == 0 {
			b[i] = 'z' // a recurring byte, so MTF has something to find
		}
	}
	return b
}

// TestTheStandardLibraryDecodesWhatWeWrote is the first judge, and it runs
// everywhere.
//
// compress/bzip2 is a decoder written from the format by someone else, and it
// CHECKS both checksums. An archive it reads back byte for byte is one whose
// block header, symbol map, tables, symbols, block CRC and stream CRC all agree.
func TestTheStandardLibraryDecodesWhatWeWrote(t *testing.T) {
	for _, c := range corpus {
		t.Run(c.name, func(t *testing.T) {
			for _, level := range []int{1, 9} {
				got, err := io.ReadAll(stdbzip2.NewReader(bytes.NewReader(compressed(t, c.data, level))))
				if err != nil {
					t.Fatalf("level %d: the standard library refused it: %v", level, err)
				}
				if !bytes.Equal(got, c.data) {
					t.Errorf("level %d: read back %d bytes, want %d", level, len(got), len(c.data))
				}
			}
		})
	}
}

// TestTheReferenceImplementationDecodesWhatWeWrote is the second judge.
//
// It is the program every bzip2 file in the world was written by, and it is the
// one whose acceptance means the archive is a bzip2 archive rather than
// something two Go packages agree about.
func TestTheReferenceImplementationDecodesWhatWeWrote(t *testing.T) {
	bin, err := exec.LookPath("bzip2")
	if err != nil {
		t.Skip("no bzip2 binary here to judge the archive with")
	}
	for _, c := range corpus {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command(bin, "-dc")
			cmd.Stdin = bytes.NewReader(compressed(t, c.data, 9))
			var out, errOut bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &errOut
			if err := cmd.Run(); err != nil {
				t.Fatalf("bzip2 -dc refused it: %v\n%s", err, errOut.String())
			}
			if !bytes.Equal(out.Bytes(), c.data) {
				t.Errorf("read back %d bytes, want %d", out.Len(), len(c.data))
			}
			// -t checks the checksums and says so, which "it decoded" does not
			// distinguish from "it decoded and the CRC was wrong but nobody
			// looked".
			f, err := os.CreateTemp(t.TempDir(), "*.bz2")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write(compressed(t, c.data, 9)); err != nil {
				t.Fatal(err)
			}
			f.Close()
			if o, err := exec.Command(bin, "-t", f.Name()).CombinedOutput(); err != nil {
				t.Errorf("bzip2 -t failed: %v\n%s", err, o)
			}
		})
	}
}
