package update

import (
	"encoding/hex"
	"strings"
	"testing"
)

// Reference digests: the first two are the published BLAKE2b-512 vectors for ""
// and "abc"; the others were produced with the reference implementation
// (golang.org/x/crypto/blake2b) and cover the block boundary (128 bytes exactly,
// 129 bytes) and a multi-block input, which is where an off-by-one in the block
// loop or in the byte counter shows up.
func TestBlake2b512KnownVectors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "empty",
			input: []byte{},
			want:  "786a02f742015903c6c6fd852552d272912f4740e15847618a86e217f71f5419d25e1031afee585313896444934eb04b903a685b1448b755d56f701afe9be2ce",
		},
		{
			name:  "abc",
			input: []byte("abc"),
			want:  "ba80a53f981c4d0d6a2797b69f12f6e94c212f14685ac4b74b12bb6fdbffa2d17d87c5392aab792dc252d5de4533cc9518d38aa8dbf1925ab92386edd4009923",
		},
		{
			name:  "exactly one block",
			input: make([]byte, 128),
			want:  "865939e120e6805438478841afb739ae4250cf372653078a065cdcfffca4caf798e6d462b65d658fc165782640eded70963449ae1500fb0f24981d7727e22c41",
		},
		{
			name:  "one block plus one byte",
			input: make([]byte, 129),
			want:  "a60edba343e7a6933c14d203d2e535f35e6deb6c8a4f8e624c1a6f6e2612860447cb4c37e5aa11bcf03b7c3eea7228eb8b998f922794f2d1b8f2dc63f03bd3fa",
		},
		{
			name:  "multi block",
			input: []byte(strings.Repeat("a", 1000)),
			want:  "d6a69459fe93fc6b9537ed4336e5099e0dcca3e97290a412500ed7a0daffb03d80cf3650a20e0591f748e10c3c534945ee83d5f2c9722f1a68d98b8c01af23fd",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := blake2b512(tc.input)
			if hex.EncodeToString(got[:]) != tc.want {
				t.Fatalf("blake2b512(%s) = %s, want %s", tc.name, hex.EncodeToString(got[:]), tc.want)
			}
		})
	}
}
