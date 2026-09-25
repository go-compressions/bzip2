// Copyright (c) 2026, go-compressions
// SPDX-License-Identifier: BSD-3-Clause

package bzip2

// ⛔ This is CRC-32/BZIP2, which is NOT the CRC-32 everything else uses.
//
// The common one (hash/crc32.IEEE) is REFLECTED: it walks bits from the least
// significant end and uses the bit-reversed polynomial. bzip2's is
// unreflected -- the polynomial 0x04C11DB7 applied from the most significant
// end -- so the two give completely different values for the same bytes, and a
// decoder that checks the checksum rejects the archive with no hint that the
// only thing wrong was which CRC.
//
// The table and update below are the same shape the standard library's bzip2
// DECODER uses, which is the thing that will check them.
var crcTable [256]uint32

func init() {
	const poly = 0x04C11DB7
	for i := range crcTable {
		crc := uint32(i) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ poly
			} else {
				crc <<= 1
			}
		}
		crcTable[i] = crc
	}
}

// updateCRC folds b into val. The initial value is 0.
func updateCRC(val uint32, b []byte) uint32 {
	crc := ^val
	for _, v := range b {
		crc = crcTable[byte(crc>>24)^v] ^ crc<<8
	}
	return ^crc
}

// combineCRC folds a block's CRC into the stream's.
//
// It is a rotate and an xor, not a sum: the stream CRC is defined as this exact
// fold, so it cannot be recomputed from the whole input afterwards.
func combineCRC(stream, block uint32) uint32 {
	return (stream<<1 | stream>>31) ^ block
}
