<p align="center"><img src="https://raw.githubusercontent.com/go-compressions/brand/main/social/go-compressions-bzip2.png" alt="go-compressions/bzip2" width="720"></p>

# bzip2

The **encoder** Go's standard library does not have — pure Go, `CGO_ENABLED=0`,
builds for every target Go builds for.

```go
z := bzip2.NewWriter(f)
io.Copy(z, src)
z.Close()          // the stream is complete only after this returns
```

`compress/bzip2` decompresses and nothing more, so a Go program that can *read* a
`.tar.bz2` cannot *write* one, and the usual answer is to shell out to the bzip2
binary: a C program, an external dependency, and absent on most cross-compilation
targets.

There is **no decoder here on purpose**. The standard library's is correct,
maintained and already everywhere; a second one would be a duplicate whose only
distinction is being ours.

## Levels are a block size

`NewWriterLevel(w, 1..9)` sets the block size, 100 KiB to 900 KiB — not an effort
setting. bzip2 does the same work either way; a bigger block just gives the
transform more to find redundancy in.

## How good is it

Smaller than the input, and bigger than the reference:

| | 106 KB of English-like text |
|---|---|
| `bzip2 -9` | 11 526 bytes |
| this | 13 123 bytes (+13.9%) |

The gap is one thing, and it is named rather than hidden. bzip2 may use up to
**six** Huffman tables per block and spends four passes deciding which group of
fifty symbols each codes best. This writer emits the **two** the format requires
as a minimum, identical, with every selector choosing the first. Adding the
iteration is the obvious next step and changes no bit of the framing around it.

## Verified by other people's decoders

Every archive in the tests is read back by two decoders this project did not
write:

- **`compress/bzip2`**, which checks both checksums;
- **the reference `bzip2` binary**, including `bzip2 -t`, which verifies rather
  than merely parses.

An **empty stream is compared byte for byte** with the reference's, at three
levels: an empty bzip2 file has no block in it, so nothing is left to choose and
the two outputs must be the same bytes.

Beyond that, the tests assert what no decoder can: that something was actually
**compressed**. A writer that stored its input verbatim inside a valid frame
would satisfy both judges.

## Two defects worth naming

**The checksum is per BLOCK, and a block can close in the middle of a `Write`.**
Folding the whole call's bytes up front credits the next block's bytes to this
one. Every single-block test passed — with one block the two are the same bytes —
and the standard library said `block checksum mismatch` the moment a second block
existed.

**Rotations can be equal.** The prefix-doubling sort stops when no class grew,
because rotations still sharing a class *are* the same string and no longer prefix
separates them. Without that, `h` doubles past `n` and the shift goes out of
bounds — three identical bytes were enough.

## Licence

BSD-3-Clause.
