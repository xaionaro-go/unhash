//go:build !go1.19
// +build !go1.19

package unhash

import "sync/atomic"

type atomicUint32 uint32
type atomicUint64 uint64

func (v *atomicUint32) Load() uint32            { return atomic.LoadUint32((*uint32)(v)) }
func (v *atomicUint32) Store(val uint32)        { atomic.StoreUint32((*uint32)(v), val) }
func (v *atomicUint32) Swap(new uint32) uint32  { return atomic.SwapUint32((*uint32)(v), new) }
func (v *atomicUint32) Add(delta uint32) uint32 { return atomic.AddUint32((*uint32)(v), delta) }
func (v *atomicUint32) CompareAndSwap(old, new uint32) bool {
	return atomic.CompareAndSwapUint32((*uint32)(v), old, new)
}

func (v *atomicUint64) Load() uint64            { return atomic.LoadUint64((*uint64)(v)) }
func (v *atomicUint64) Store(val uint64)        { atomic.StoreUint64((*uint64)(v), val) }
func (v *atomicUint64) Swap(new uint64) uint64  { return atomic.SwapUint64((*uint64)(v), new) }
func (v *atomicUint64) Add(delta uint64) uint64 { return atomic.AddUint64((*uint64)(v), delta) }
func (v *atomicUint64) CompareAndSwap(old, new uint64) bool {
	return atomic.CompareAndSwapUint64((*uint64)(v), old, new)
}
