//go:build go1.19
// +build go1.19

package unhash

import "sync/atomic"

type atomicUint32 = atomic.Uint32
type atomicUint64 = atomic.Uint64
