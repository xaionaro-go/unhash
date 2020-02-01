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

func estimateTime(timeSpent time.Duration, curPos, totalLength uint) time.Duration {
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
}

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

	startedAt := time.Now()
	curPos := 0
	for curPos < len(fileData) {
		var wg sync.WaitGroup

		numTasks := runtime.NumCPU()
		if len(fileData)-curPos < numTasks {
			numTasks = len(fileData) - curPos
		}
		wg.Add(numTasks)
		for i := 0; i < numTasks; i++ {
			go func(startPos int) {
				defer fmt.Print("E")
				defer wg.Done()

				hashInstance := sha1.New()
				for idx, b := range fileData[startPos:] {
					if (idx+1)&0xfffff == 0xfffff {
						fmt.Print(".")
					}
					hashInstance.Write([]byte{b})
					hashValue := hashInstance.Sum(nil)
					if bytes.Compare(hashValue, neededHash) == 0 {
						found(fileData[startPos : startPos+idx+1])
					}
					if bytes.Compare(hashValue, neededHashReversed) == 0 {
						found(fileData[startPos : startPos+idx+1])
					}
				}
			}(curPos + i)
		}

		curPos += numTasks
		wg.Wait()

		timeSpent := time.Since(startedAt)

		fmt.Printf(" %d/%d: (%v / %v)\n",
			curPos, len(fileData),
			timeSpent, estimateTime(timeSpent, uint(curPos), uint(len(fileData))),
		)
	}

	fmt.Println("did not find :(")
}
