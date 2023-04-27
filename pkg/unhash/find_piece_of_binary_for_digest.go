package unhash

import (
	"context"
	"fmt"
	"hash"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/facebookincubator/go-belt/beltctx"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"golang.org/x/exp/constraints"
	"golang.org/x/sync/semaphore"
)

type SearchInBinaryBlobSettings struct {
	Ranges     []SearchInBinaryBlobRange
	MaxGuesses uint64
}

type SearchInBinaryBlobRange struct {
	StartPosStep    uint
	IterationStep   uint
	SkipShorterThan uint
	SkipLongerThan  uint
}

func SearchInBinaryBlobSettingsForSize(
	blob []byte,
	hasher hash.Hash,
	dataSize uint,
) *SearchInBinaryBlobSettings {
	return &SearchInBinaryBlobSettings{
		Ranges: []SearchInBinaryBlobRange{
			{1, dataSize, dataSize, dataSize},
			{4, dataSize, dataSize, dataSize},
			{16, dataSize, dataSize, dataSize},
			{64, dataSize, dataSize, dataSize},
			{256, dataSize, dataSize, dataSize},
			{1024, dataSize, dataSize, dataSize},
			{1 << 12, dataSize, dataSize, dataSize},
			{1 << 14, dataSize, dataSize, dataSize},
			{1 << 16, dataSize, dataSize, dataSize},
			{1 << 18, dataSize, dataSize, dataSize},
			{1 << 20, dataSize, dataSize, dataSize},
		},
	}
}

func DefaultSearchInBinaryBlobSettings(
	blob []byte,
	hasher hash.Hash,
) *SearchInBinaryBlobSettings {
	hs := uint(hasher.Size())
	bs := uint(hasher.BlockSize())
	return &SearchInBinaryBlobSettings{
		Ranges: []SearchInBinaryBlobRange{
			{1, bs, bs, 0},
			{1, bs, bs, max(bs, 0x100)},
			{1, 1, 0, 0},
			{1, 1, 0, 128},
			{1, 1, 128, 1024},
			{1, 1, 1024, 1024 * 1024},
			{1, 1, 1024 * 1024, 8 * 1024 * 1024},
			{1, 1, 8 * 1024 * 1024, 0},
			{4, 4, 0, 64},
			{4, 4, 64, 1024 * 1024},
			{8, 8, 1024 * 1024, 4 * 1024 * 1024},
			{4, bs, bs, bs},
			{4, hs, hs, hs},
			{16, 16, 0, 64},
			{16, 16, 64, 1024 * 1024},
			{16, 16, 1024 * 1024, 4 * 1024 * 1024},
			{0x100, 1, 0, min(bs, 0x100)},
			{0x100, min(bs, 0x100), min(bs, 0x100), 0x1000},
			{0x100, min(bs, 0x100), 0x1000, 1024 * 1024},
			{0x100, min(bs, 0x100), 1024 * 1024, 0},
			{0x1000, bs, bs, 0x1000},
			{0x1000, bs, 0x1000, 0},
			{0x10000, bs, bs, 0x10000},
			{0x10000, bs, 0x10000, 0},
			{0x100000, bs, bs, 0x100000},
			{0x100000, bs, 0x100000, 0},
			{0x1000000, bs, bs, 0x1000000},
			{0x1000000, bs, 0x1000000, 0},
		},
	}
}

/*func estimateTime(timeSpent time.Duration, curPos, totalLength uint) time.Duration {
	// The further we go, the faster it will be (linearly), so:
	goneThrough := float64(curPos) / float64(totalLength)

	// in the end: T = C * totalLength * (totalLength-1) / 2
	// in the progress: T = C * totalLength * (totalLength - 1) * goneThrough -
	//                      - totalLength * goneThrough * (totalLength - 1) * goneThrough / 2
	// simplify: T = C * (totalLength * (totalLength - 1)) * goneThrough * (1 - gnomeThrough / 2)
	// Getting "C":
	// C = T / ( (totalLength * (totalLength - 1)) * goneThrough * (1 - gnomeThrough / 2) )

	t := float64(timeSpent.Nanoseconds())
	l := float64(totalLength)
	g := goneThrough
	coefficient := t / ( (l * (l-1)) * g * (1 - g / 2) )
	return time.Nanosecond * time.Duration(coefficient * l * (l - 1) / 2)
}*/

type HasherFactory func() hash.Hash

type BinaryPieceCheckFunc func(ctx context.Context, hashValue Digest, startPos, endPos uint) bool

type pieceFinder struct {
	// immutable:
	foundFunc     BinaryPieceCheckFunc
	binaryBytes   []byte
	hasherFactory HasherFactory
	settings      *SearchInBinaryBlobSettings
}

type Digest = []byte

func newPieceFinder(
	foundFunc BinaryPieceCheckFunc,
	binaryBytes []byte,
	hasherFactory HasherFactory,
	settings *SearchInBinaryBlobSettings,
) (*pieceFinder, error) {
	if settings == nil {
		settings = DefaultSearchInBinaryBlobSettings(binaryBytes, hasherFactory())
	}

	for idx, r := range settings.Ranges {
		if r.StartPosStep == 0 {
			return nil, fmt.Errorf("range #%d (%#+v) in settings has invalid starting position step size: zero; should be at least one", idx, r)
		}
		if r.IterationStep == 0 {
			return nil, fmt.Errorf("range #%d (%#+v) in settings has invalid iteration step size: zero; should be at least one", idx, r)
		}
	}

	return &pieceFinder{
		foundFunc:     foundFunc,
		binaryBytes:   binaryBytes,
		hasherFactory: hasherFactory,
		settings:      settings,
	}, nil
}

func FindPieceOfBinaryForDigest(
	ctx context.Context,
	foundFunc BinaryPieceCheckFunc,
	binaryBytes []byte,
	hasherFactory HasherFactory,
	settings *SearchInBinaryBlobSettings,
) (bool, uint64, error) {
	pieceFinder, err := newPieceFinder(foundFunc, binaryBytes, hasherFactory, settings)
	if err != nil {
		return false, 0, err
	}

	return pieceFinder.Execute(ctx)
}

type findStatus uint32

const (
	findStatusNotFound = findStatus(iota)
	findStatusFound
	findStatusCancelled
)

type pieceFinderWorkerShared struct {
	GuessCount atomic.Uint64

	// Status is a global signaler if somebody already
	// found a solution and everybody else should stop wasting CPU.
	//
	// It is preferred over using Context signaling due to performance reasons.
	Status atomic.Uint32
}

func (f *pieceFinder) Execute(
	ctx context.Context,
) (bool, uint64, error) {
	shared := pieceFinderWorkerShared{}

	result := executeWorkers(
		ctx,
		nil,
		f.settings.Ranges,
		f.executeWorker,
		f.aggregateWorkerResults,
		&shared,
	)
	return findStatus(shared.Status.Load()) == findStatusFound, shared.GuessCount.Load(), result.Error
}

func executeWorkers[job any, result any, sharedData any](
	ctx context.Context,
	sem *semaphore.Weighted,
	jobs []job,
	workerExec func(context.Context, *job, sharedData) result,
	aggregateResults func(results <-chan result) result,
	shared sharedData,
) result {
	var wg sync.WaitGroup

	ctx = beltctx.WithField(ctx, "shared_data", shared)
	ctx, cancelFn := context.WithCancel(ctx)

	workerResultCh := make(chan result)
	for _, j := range jobs {
		if sem != nil {
			err := sem.Acquire(ctx, 1)
			if err != nil {
				logger.FromCtx(ctx).Panic(err)
			}
		}
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			if sem != nil {
				defer sem.Release(1)
			}
			ctx := beltctx.WithField(ctx, "job", j)
			workerResultCh <- workerExec(ctx, &j, shared)
		}(j)
	}

	go func() {
		wg.Wait()
		cancelFn()
		close(workerResultCh)
	}()

	return aggregateResults(workerResultCh)
}

type pieceFinderWorkerResult struct {
	Error error
}

func (f *pieceFinder) aggregateWorkerResults(
	resultChan <-chan pieceFinderWorkerResult,
) pieceFinderWorkerResult {
	var result pieceFinderWorkerResult

	var errors *multierror.Error
	for r := range resultChan {
		errors = multierror.Append(errors, r.Error)
	}

	result.Error = errors.ErrorOrNil()
	return result
}

type subWorkerJob struct {
	startStartPos uint
	startEndPos   uint
}

func (f *pieceFinder) executeWorker(
	ctx context.Context,
	rangeCfg *SearchInBinaryBlobRange,
	shared *pieceFinderWorkerShared,
) (result pieceFinderWorkerResult) {
	logger.FromCtx(ctx).Debugf("started bruteforce")
	defer func() {
		logger.FromCtx(ctx).Debugf("ended bruteforce; result: %#+v", result)
	}()

	if shared.Status.Load() != 0 {
		return
	}

	numJobs := uint(runtime.NumCPU())
	if uint(len(f.binaryBytes))/uint(rangeCfg.StartPosStep) < numJobs {
		numJobs = uint(len(f.binaryBytes)) / uint(rangeCfg.StartPosStep)
		if numJobs == 0 {
			numJobs = 1
		}
	}

	iterationsPerJob := ((uint(len(f.binaryBytes)) + (rangeCfg.StartPosStep - 1)) / rangeCfg.StartPosStep) / numJobs
	if iterationsPerJob == 0 {
		iterationsPerJob = 1
	}

	jobs := make([]subWorkerJob, 0, numJobs)
	curStartStartPos := uint(0)
	for {
		curEndStartPos := curStartStartPos + iterationsPerJob*rangeCfg.StartPosStep
		if curEndStartPos > uint(len(f.binaryBytes)) {
			curEndStartPos = uint(len(f.binaryBytes))
		}
		jobs = append(jobs, subWorkerJob{
			startStartPos: curStartStartPos,
			startEndPos:   curEndStartPos,
		})
		curStartStartPos = curEndStartPos
		if curStartStartPos >= uint(len(f.binaryBytes)) {
			break
		}
	}

	return executeWorkers(
		ctx,
		nil,
		jobs,
		f.executeSubWorker,
		f.aggregateWorkerResults,
		subWorkerSharedData{
			Global:      shared,
			RangeConfig: rangeCfg,

			IsTracingEnabled: logger.FromCtx(ctx).Level() >= logger.LevelTrace,
		},
	)
}

type subWorkerSharedData struct {
	Global      *pieceFinderWorkerShared
	RangeConfig *SearchInBinaryBlobRange

	// debug:
	IsTracingEnabled bool
}

func (f *pieceFinder) executeSubWorker(
	ctx context.Context,
	job *subWorkerJob,
	shared subWorkerSharedData,
) (result pieceFinderWorkerResult) {
	if shared.IsTracingEnabled {
		logger.FromCtx(ctx).Tracef("started subworker")
		defer func() {
			logger.FromCtx(ctx).Tracef("ended subworker; result == %#+v", result)
		}()
	}

	rangeCfg := shared.RangeConfig
	status := &shared.Global.Status
	guessCount := &shared.Global.GuessCount
	guessLimit := f.settings.MaxGuesses
	binaryBytes := f.binaryBytes
	blobSize := uint(len(binaryBytes))
	startPosStep := rangeCfg.StartPosStep
	iterationStep := rangeCfg.IterationStep
	skipShorterThan := rangeCfg.SkipShorterThan
	skipLongerThan := rangeCfg.SkipLongerThan
	foundFunc := f.foundFunc

	hashInstance := f.hasherFactory()
	hashValue := make([]byte, hashInstance.Size())

	for startHashPos := job.startStartPos; startHashPos < job.startEndPos; startHashPos += startPosStep {
		hashInstance.Reset()
		endPos := blobSize
		if skipLongerThan > 0 {
			endPos = min(endPos, startHashPos+skipLongerThan+(iterationStep-1))
		}

		startIteratePos := startHashPos
		if skipShorterThan > 0 {
			if startHashPos+skipShorterThan > endPos {
				break
			}
			hashInstance.Write(binaryBytes[startHashPos : startHashPos+skipShorterThan])
			startIteratePos += skipShorterThan
			hashValue = hashValue[:0]
			hashValue = hashInstance.Sum(hashValue)
			if shared.IsTracingEnabled {
				logger.FromCtx(ctx).Tracef("%X-%X: %X", startHashPos, startIteratePos, hashValue)
			}

			if v := guessCount.Add(1); guessLimit != 0 && v >= guessLimit {
				status.CompareAndSwap(uint32(findStatusNotFound), uint32(findStatusCancelled))
				return
			}
			if foundFunc(ctx, hashValue, startHashPos, startIteratePos) {
				status.Store(uint32(findStatusFound))
				return
			}
		}
		if shared.IsTracingEnabled {
			ctx = beltctx.WithFields(ctx, field.Map[uint]{
				"start_pos":      startHashPos,
				"start_pos_iter": startIteratePos,
				"end_pos":        endPos,
				"step":           iterationStep,
			})
			logger.FromCtx(ctx).Tracef("bruteforcing range")
		}

		for idx := startIteratePos; ; idx += iterationStep {
			if status.Load() != 0 {
				return
			}

			localEndIdx := idx + iterationStep
			if localEndIdx > endPos {
				break
			}
			hashInstance.Write(binaryBytes[idx:localEndIdx])
			hashValue = hashValue[:0]
			hashValue = hashInstance.Sum(hashValue)
			if shared.IsTracingEnabled {
				logger.FromCtx(ctx).Tracef("%X-%X: %X", startHashPos, localEndIdx, hashValue)
			}

			if v := guessCount.Add(1); guessLimit != 0 && v >= guessLimit {
				status.CompareAndSwap(uint32(findStatusNotFound), uint32(findStatusCancelled))
				return
			}
			if foundFunc(ctx, hashValue, startHashPos, localEndIdx) {
				status.Store(1)
				return
			}
		}
	}
	return
}

func min[T constraints.Integer](v0 T, vs ...T) T {
	vMin := v0
	for _, vCmp := range vs {
		if vCmp < vMin {
			vMin = vCmp
		}
	}
	return vMin
}

func max[T constraints.Integer](v0 T, vs ...T) T {
	vMax := v0
	for _, vCmp := range vs {
		if vCmp > vMax {
			vMax = vCmp
		}
	}
	return vMax
}
