// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

import (
	"bytes"
	"errors"
	"testing"
)

// failAfter is a writer that accepts n bytes and then refuses everything.
//
// The point is not that an error comes back but WHERE from: a compressor holds
// output in a bit buffer, so the write that fails is rarely the call the caller
// made, and an error dropped on the floor there produces a truncated archive
// that reports success.
type failAfter struct {
	left int
	err  error
}

func (f *failAfter) Write(p []byte) (int, error) {
	if f.left <= 0 {
		return 0, f.err
	}
	if len(p) > f.left {
		n := f.left
		f.left = 0
		return n, f.err
	}
	f.left -= len(p)
	return len(p), nil
}

func TestAFailingWriterIsReported(t *testing.T) {
	boom := errors.New("the disk filled up")
	// Enough input to force several drains, so the failure lands inside Write
	// rather than only at Close.
	data := pattern(200_000)

	for _, after := range []int{0, 1, 100, 4096, 20_000} {
		// ⛔ LEVEL 1, so blocks are 100 KB and this input crosses several of
		// them. At the default level the whole 200 KB fits in one block, no
		// block is ever flushed from inside Write, and the error path there --
		// the one that matters, because it is where a caller learns the archive
		// is doomed before Close -- is never taken. The first version of this
		// test used NewWriter and left exactly that statement uncovered.
		w := &failAfter{left: after, err: boom}
		z, err := NewWriterLevel(w, 1)
		if err != nil {
			t.Fatal(err)
		}
		_, wErr := z.Write(data)
		cErr := z.Close()
		if !errors.Is(wErr, boom) && !errors.Is(cErr, boom) {
			t.Errorf("after %d bytes: Write=%v Close=%v, want %v from one of them",
				after, wErr, cErr, boom)
		}
	}
}

// TestClosingAWriterThatWroteNothingStillEndsTheStream.
//
// The bit buffer is empty and there are no pending bits, so the flush at the end
// has nothing to hand over -- and must not hand over a zero-length write either,
// which some io.Writers treat as a signal.
func TestClosingAWriterThatWroteNothingStillEndsTheStream(t *testing.T) {
	var w countingWriter
	z := NewWriter(&w)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if w.empty > 0 {
		t.Errorf("%d zero-length writes reached the underlying writer", w.empty)
	}
	if w.bytes == 0 {
		t.Error("closing wrote nothing at all, and an empty bzip2 stream is 14 bytes")
	}
}

// countingWriter counts what it is handed, including the calls handing it
// nothing.
type countingWriter struct {
	bytes int
	empty int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		c.empty++
	}
	c.bytes += len(p)
	return len(p), nil
}

// TestDrainWithNothingPendingIsANoOp exercises the bit writer directly: close on
// a writer nothing was given must not write, and must not error.
func TestDrainWithNothingPendingIsANoOp(t *testing.T) {
	var w countingWriter
	b := newBitWriter(&w)
	if err := b.close(); err != nil {
		t.Fatal(err)
	}
	if w.bytes != 0 || w.empty != 0 {
		t.Errorf("closing an untouched bit writer wrote %d bytes in %d empty calls",
			w.bytes, w.empty)
	}
	// And a second drain, still with nothing pending, is also silent.
	b.drain()
	if w.bytes != 0 {
		t.Errorf("draining twice wrote %d bytes", w.bytes)
	}
}

func TestLevelsOutsideTheRangeAreRefused(t *testing.T) {
	for _, level := range []int{-1, 0, 10, 99} {
		if _, err := NewWriterLevel(&bytes.Buffer{}, level); err == nil {
			t.Errorf("level %d was accepted", level)
		}
	}
	for level := 1; level <= 9; level++ {
		if _, err := NewWriterLevel(&bytes.Buffer{}, level); err != nil {
			t.Errorf("level %d was refused: %v", level, err)
		}
	}
}

// TestAClosedWriterRefusesEverything. Close writes the stream trailer, so a Write
// after it would append bytes AFTER the end of the archive -- which most decoders
// ignore, silently, so the data would simply be gone.
func TestAClosedWriterRefusesEverything(t *testing.T) {
	var buf bytes.Buffer
	z := NewWriter(&buf)
	if _, err := z.Write([]byte("a body")); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	sealed := buf.Len()

	if _, err := z.Write([]byte("more")); !errors.Is(err, ErrClosed) {
		t.Errorf("Write after Close = %v, want ErrClosed", err)
	}
	if err := z.Close(); !errors.Is(err, ErrClosed) {
		t.Errorf("second Close = %v, want ErrClosed", err)
	}
	if buf.Len() != sealed {
		t.Errorf("the stream grew after Close: %d -> %d", sealed, buf.Len())
	}
}
