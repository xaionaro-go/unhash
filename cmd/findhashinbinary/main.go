package main

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"syscall"

	"github.com/edsrzf/mmap-go"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/unhash/pkg/unhash"
)

func fatalIfError(ctx context.Context, err error) {
	if err == nil {
		return
	}

	logger.FromCtx(ctx).Fatal(err)
	os.Exit(2)
}

func syntaxFatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "error: "+format+"\n\n", args...)
	_, _ = fmt.Fprintf(os.Stderr, "syntax: %s hash_func hash_in_hex /path/to/file.bin\n", os.Args[0])
	os.Exit(int(syscall.EINVAL))
}

func main() {
	logLevel := logger.LevelWarning
	flag.Var(&logLevel, "log-level", "")
	expectSizeFlag := flag.Uint("expect-size", 0, "")
	netPprofFlag := flag.String("net-pprof", "", "")
	flag.Parse()

	if flag.NArg() != 3 {
		syntaxFatalf("required exactly 3 arguments, but received %d", flag.NArg())
	}

	ctx := logger.CtxWithLogger(context.Background(), logrus.Default().WithLevel(logLevel))

	if *netPprofFlag != "" {
		go func() {
			logger.FromCtx(ctx).Error(http.ListenAndServe(*netPprofFlag, nil))
		}()
	}

	hashFuncName := flag.Arg(0)
	digestHex := flag.Arg(1)
	binaryFilePath := flag.Arg(2)

	hashFuncName = strings.Trim(strings.ToLower(hashFuncName), " ")

	var hasherFactory unhash.HasherFactory
	switch hashFuncName {
	case "sha1":
		hasherFactory = sha1.New
	case "sha256":
		hasherFactory = sha256.New
	case "sha512":
		hasherFactory = sha512.New
	default:
		panic(fmt.Errorf("unknown/unsupported hash function: '%s'", hashFuncName))
	}

	digest, err := hex.DecodeString(digestHex)
	fatalIfError(ctx, err)

	binaryBytes, err := fileToBytes(binaryFilePath)
	fatalIfError(ctx, err)

	var settings *unhash.SearchInBinaryBlobSettings
	if *expectSizeFlag != 0 {
		settings = unhash.SearchInBinaryBlobSettingsForSize(binaryBytes, hasherFactory(), *expectSizeFlag)
	}

	startPos, endPos := uint(0), uint(0)
	found, checkCount, err := unhash.FindPieceOfBinaryForDigest(
		ctx,
		unhash.FindDigestSourceAnyDigest(ctx, &startPos, &endPos, digest),
		binaryBytes,
		hasherFactory,
		settings,
	)
	fatalIfError(ctx, err)

	fmt.Println("Checked ", checkCount, " hash values")
	fmt.Println()
	if !found {
		fmt.Printf("have not found data which hashes into %X with %s\n", digest, hashFuncName)
		return
	}

	fmt.Println("Found!")
	fmt.Println()
	fmt.Printf("start_pos = %d (0x%X)\n", startPos, startPos)
	fmt.Printf("  end_pos = %d (0x%X)\n", endPos, endPos)
	if endPos-startPos <= uint(65536) {
		fmt.Println()
		fmt.Printf("the data to be hashed is: %X\n", binaryBytes[startPos:endPos])
	}
}

// fileToBytes returns the contents of the file by path `filePath`.
func fileToBytes(filePath string) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf(`unable to open the image-file "%v": %w`,
			filePath, err)
	}
	defer file.Close() // it was a read-only Open(), so we don't check the Close()

	// To consume less memory we use mmap() instead of reading the image
	// into the memory. However these bytes are also parsed by
	// linuxboot/fiano/pkg/uefi which consumes a lot of memory anyway :(
	//
	// See "man 2 mmap".
	contents, err := mmap.Map(file, mmap.RDONLY, 0)
	if err == nil {
		return contents, nil
	}

	// An error? OK, let's try the usual way to read data:
	contents, err = io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf(`unable to access data of the image-file "%v": %w`,
			filePath, err)
	}
	return contents, nil
}
