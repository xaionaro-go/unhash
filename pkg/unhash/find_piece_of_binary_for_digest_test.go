package unhash

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkFindPieceOfBinaryForDigest(b *testing.B) {
	for _, blobSize := range []uint{1 << 10, 1 << 20} {
		for _, offset := range []uint{0, 1, 1 << 8, 312321, 1 << 10, 1 << 16, 1 << 19} {
			for _, size := range []uint{1, 1 << 8, 1 << 10, 1 << 16} {
				for _, hashFuncName := range []string{"sha1", "sha256"} {
					endPos := offset + size
					if endPos > blobSize {
						continue
					}
					b.Run(fmt.Sprintf("blobSize-%d", blobSize), func(b *testing.B) {
						b.Run(fmt.Sprintf("offset-%d", offset), func(b *testing.B) {
							b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
								b.Run(hashFuncName, func(b *testing.B) {
									benchmarkFindPieceOfBinaryForDigest(
										b,
										blobSize, offset, size,
										hashFuncName,
									)
								})
							})
						})
					})
				}
			}
		}
	}
}

func benchmarkFindPieceOfBinaryForDigest(
	b *testing.B,
	blobSize, offset, size uint,
	hashFuncName string,
) {
	blob := make([]byte, blobSize)
	rng := rand.New(rand.NewSource(0))
	_, err := rng.Read(blob)
	require.NoError(b, err)

	var hasherFactory HasherFactory
	switch hashFuncName {
	case "sha1":
		hasherFactory = sha1.New
	case "sha256":
		hasherFactory = sha256.New
	default:
		b.Fatalf("unexpected hash function name: '%s'", hashFuncName)
	}

	var digest Digest
	{
		hasher := hasherFactory()
		_, err = hasher.Write(blob[offset : offset+size])
		require.NoError(b, err)
		digest = hasher.Sum(nil)
	}

	ctx := context.Background()

	var startPos, endPos uint
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		found, checkCount, err := FindPieceOfBinaryForDigest(
			ctx,
			FindDigestSourceAnyDigest(ctx, &startPos, &endPos, digest),
			blob,
			hasherFactory,
			nil,
		)
		require.True(b, found, checkCount)
		require.NoError(b, err)
		if size > 16 {
			require.Equal(b, startPos, offset)
			require.Equal(b, endPos, offset+size)
		}
	}
}
