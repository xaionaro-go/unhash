package unhash

import (
	"bytes"
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func FindDigestSourceAnyDigest(
	ctx context.Context,
	startPosOutputPtr, endPosOutputPtr *uint,
	digests ...Digest,
) BinaryPieceCheckFunc {
	if len(digests) > 100 {
		// The implementation of this function uses linear search in the digests list.
		logger.FromCtx(ctx).Warnf("this function is not supposed to be used with too many digests at once")
	}

	return func(ctx context.Context, digest Digest, startPos, endPos uint) bool {
		for _, digestCompare := range digests {
			if bytes.Equal(digest, digestCompare) {
				if startPosOutputPtr != nil {
					*startPosOutputPtr = startPos
				}
				if endPosOutputPtr != nil {
					*endPosOutputPtr = endPos
				}
				return true
			}
		}
		return false
	}
}

type FoundDigestSourceResult struct {
	DigestIndex uint
	StartPos    uint
	EndPos      uint
}

func FindDigestSourceAllDigests(
	ctx context.Context,
	foundCh chan<- FoundDigestSourceResult,
	digests ...Digest,
) BinaryPieceCheckFunc {
	if len(digests) > 100 {
		// The implementation of this function uses linear search in the digests list.
		logger.FromCtx(ctx).Warnf("this function is not supposed to be used with too many digests at once")
	}

	digestsCopy := make([]Digest, len(digests))
	for idx, digest := range digests {
		copy(digestsCopy[idx], digest)
	}
	digestsOrig := digests
	digests = digestsCopy
	return func(ctx context.Context, digest Digest, startPos, endPos uint) bool {
		for idx, digestCompare := range digests {
			if !bytes.Equal(digest, digestCompare) {
				continue
			}
			digestIndex := -1
			for idx, digestCompare := range digestsOrig {
				if bytes.Equal(digest, digestCompare) {
					digestIndex = idx
					break
				}
			}
			foundCh <- FoundDigestSourceResult{
				DigestIndex: uint(digestIndex),
				StartPos:    startPos,
				EndPos:      endPos,
			}
			digests[idx] = digests[len(digests)-1]
			digests = digests[:len(digests)-1]
			if len(digests) == 0 {
				// found all digests
				return true
			}
			break
		}
		return false
	}
}
