package update

import (
	"encoding/binary"
	"math/bits"
)

// Minimal unkeyed BLAKE2b-512 (RFC 7693).
//
// It exists only to verify prehashed minisign signatures ("ED"), which is what
// current minisign implementations produce by default. golang.org/x/crypto/blake2b
// would replace this file with one import, but the project is deliberately
// dependency-free and the standard library has no BLAKE2b.
//
// Correctness is pinned by the RFC 7693 vectors and by an end-to-end signature
// produced by a real minisign implementation, see blake2b_test.go and
// signature_test.go.

const blake2bBlockSize = 128

var blake2bIV = [8]uint64{
	0x6a09e667f3bcc908, 0xbb67ae8584caa73b, 0x3c6ef372fe94f82b, 0xa54ff53a5f1d36f1,
	0x510e527fade682d1, 0x9b05688c2b3e6c1f, 0x1f83d9abfb41bd6b, 0x5be0cd19137e2179,
}

var blake2bSigma = [12][16]byte{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
	{14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3},
	{11, 8, 12, 0, 5, 2, 15, 13, 10, 14, 3, 6, 7, 1, 9, 4},
	{7, 9, 3, 1, 13, 12, 11, 14, 2, 6, 5, 10, 4, 0, 15, 8},
	{9, 0, 5, 7, 2, 4, 10, 15, 14, 1, 11, 12, 6, 8, 3, 13},
	{2, 12, 6, 10, 0, 11, 8, 3, 4, 13, 7, 5, 15, 14, 1, 9},
	{12, 5, 1, 15, 14, 13, 4, 10, 0, 7, 6, 3, 9, 2, 8, 11},
	{13, 11, 7, 14, 12, 1, 3, 9, 5, 0, 15, 4, 8, 6, 2, 10},
	{6, 15, 14, 9, 11, 3, 0, 8, 12, 2, 13, 7, 1, 4, 10, 5},
	{10, 2, 8, 4, 7, 6, 1, 5, 15, 11, 9, 14, 3, 12, 13, 0},
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
	{14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3},
}

func blake2b512(data []byte) [64]byte {
	var h [8]uint64
	copy(h[:], blake2bIV[:])
	// Parameter block: digest length 64, key length 0, fanout 1, depth 1.
	h[0] ^= 0x01010000 ^ 64

	var counter uint64
	var block [blake2bBlockSize]byte
	for len(data) > blake2bBlockSize {
		copy(block[:], data[:blake2bBlockSize])
		counter += blake2bBlockSize
		blake2bCompress(&h, &block, counter, false)
		data = data[blake2bBlockSize:]
	}

	// The final block is zero padded; an empty input hashes a zero block with a
	// zero byte counter, as the RFC requires.
	block = [blake2bBlockSize]byte{}
	copy(block[:], data)
	counter += uint64(len(data))
	blake2bCompress(&h, &block, counter, true)

	var out [64]byte
	for i, word := range h {
		binary.LittleEndian.PutUint64(out[i*8:], word)
	}
	return out
}

func blake2bCompress(h *[8]uint64, block *[blake2bBlockSize]byte, counter uint64, last bool) {
	var m [16]uint64
	for i := range m {
		m[i] = binary.LittleEndian.Uint64(block[i*8:])
	}

	var v [16]uint64
	copy(v[:8], h[:])
	copy(v[8:], blake2bIV[:])
	v[12] ^= counter
	// v[13] holds the high half of the byte counter: an artifact never reaches
	// 2^64 bytes, so it stays zero.
	if last {
		v[14] ^= ^uint64(0)
	}

	for round := 0; round < 12; round++ {
		s := &blake2bSigma[round]
		blake2bMix(&v, 0, 4, 8, 12, m[s[0]], m[s[1]])
		blake2bMix(&v, 1, 5, 9, 13, m[s[2]], m[s[3]])
		blake2bMix(&v, 2, 6, 10, 14, m[s[4]], m[s[5]])
		blake2bMix(&v, 3, 7, 11, 15, m[s[6]], m[s[7]])
		blake2bMix(&v, 0, 5, 10, 15, m[s[8]], m[s[9]])
		blake2bMix(&v, 1, 6, 11, 12, m[s[10]], m[s[11]])
		blake2bMix(&v, 2, 7, 8, 13, m[s[12]], m[s[13]])
		blake2bMix(&v, 3, 4, 9, 14, m[s[14]], m[s[15]])
	}

	for i := 0; i < 8; i++ {
		h[i] ^= v[i] ^ v[i+8]
	}
}

func blake2bMix(v *[16]uint64, a, b, c, d int, x, y uint64) {
	v[a] = v[a] + v[b] + x
	v[d] = bits.RotateLeft64(v[d]^v[a], -32)
	v[c] = v[c] + v[d]
	v[b] = bits.RotateLeft64(v[b]^v[c], -24)
	v[a] = v[a] + v[b] + y
	v[d] = bits.RotateLeft64(v[d]^v[a], -16)
	v[c] = v[c] + v[d]
	v[b] = bits.RotateLeft64(v[b]^v[c], -63)
}
