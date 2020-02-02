package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
)

func fatalIfError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func fatalSyntax() {
	_, _ = fmt.Fprintf(os.Stderr, "syntax: %s hash_in_hex /path/to/file.bin\n", os.Args[0])
	os.Exit(int(syscall.EINVAL))
}

func found(b []byte) {
	fmt.Println(b)
	os.Exit(0)
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

func main() {
	flag.Parse()

	if flag.NArg() != 2 {
		fatalSyntax()
	}

	neededHash, err := hex.DecodeString(flag.Arg(0))
	fatalIfError(err)

	if len(neededHash) != 20 {
		panic("invalid hash length, SHA1 hash has exactly 20 bytes")
	}

	// May be the hash was entered in the reverse order due to LittleEndian-vs-BigEndian problems,
	// so we also remember the reverse one.
	neededHashReversed := make([]byte, len(neededHash))
	for idx, b := range neededHash {
		neededHashReversed[len(neededHash)-1-idx] = b
	}

	fileData, err := ioutil.ReadFile(flag.Arg(1))
	fatalIfError(err)

	lengthLimitsCollection := []struct {
		startStep   uint
		iterateStep uint
		skipLow     uint
		highLimit   uint
	}{
		{1, 1, 0, 128},
		{1, 1, 128, 1024},
		{1, 1, 1024, 1024 * 1024},
		{8, 8, 1024 * 1024, 0},
		{1, 1, 1024 * 1024, 0},
	}

	var wg sync.WaitGroup
	startedAt := time.Now()
	for _, lengthLimits := range lengthLimitsCollection {
		wg.Add(1)
		go func(startStep, iterateStep, highLimit, skipLow uint) {
			defer wg.Done()

			curPos := 0
			for curPos < len(fileData) {
				var wg sync.WaitGroup

				numTasks := runtime.NumCPU()
				if (len(fileData)-curPos)/int(iterateStep) < numTasks {
					numTasks = (len(fileData) - curPos) / int(iterateStep)
				}
				wg.Add(numTasks)
				for i := 0; i < numTasks; i++ {
					go func(startPos int) {
						if highLimit == 0 {
							defer fmt.Print("E")
						}
						defer wg.Done()

						endPos := len(fileData)
						if highLimit > 0 {
							endPos = startPos + int(highLimit) + int(iterateStep)
						}
						hashInstance := sha1.New()

						startIteratePos := startPos
						if skipLow > 0 {
							hashInstance.Write(fileData[startPos : startPos+int(skipLow)])
							startIteratePos += int(skipLow)
						}
						for idx := startIteratePos; idx < endPos; idx += int(iterateStep) {
							localEndIdx := idx + int(iterateStep)
							if highLimit == 0 && localEndIdx&0xfffff == 0 {
								fmt.Print(".")
							}
							hashInstance.Write(fileData[idx:localEndIdx])
							hashValue := hashInstance.Sum(nil)
							if bytes.Compare(hashValue, neededHash) == 0 {
								found(fileData[startPos:localEndIdx])
							}
							if bytes.Compare(hashValue, neededHashReversed) == 0 {
								found(fileData[startPos:localEndIdx])
							}
						}
					}(curPos + i*int(startStep))
				}

				curPos += numTasks * int(startStep)
				wg.Wait()

				timeSpent := time.Since(startedAt)

				if highLimit == 0 || highLimit >= 65536 || timeSpent.Nanoseconds()%0xff == 0 {
					fmt.Printf("maxSize:%d;startStep:%d;iterateStep:%d %d/%d: (%v)\n",
						highLimit, startStep, iterateStep,
						curPos, len(fileData),
						timeSpent,
					)
				}
			}
		}(lengthLimits.startStep, lengthLimits.iterateStep, lengthLimits.highLimit, lengthLimits.skipLow)
	}

	wg.Wait()
	fmt.Println("did not find :(")
}
