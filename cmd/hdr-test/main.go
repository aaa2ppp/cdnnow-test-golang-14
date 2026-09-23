package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strconv"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/operators"

	"github.com/HdrHistogram/hdrhistogram-go"
)

// Usage: CGO_ENABLED=1 go run ./cmd/hdr-test [N]

func main() {
	mustLoadLibraries()
	const N = 10_000
	const MaxValue = int64(time.Millisecond)

	count := N
	if len(os.Args) > 1 {
		countStr := os.Args[1]
		var err error
		count, err = strconv.Atoi(countStr)
		if err != nil {
			log.Fatal(err)
		}
	}

	var sum int64
	var errCount int
	lH := hdrhistogram.New(1, MaxValue, 3)

	start := time.Now()
	for i := 0; i < count; i++ {
		num := rand.Int64N(201) - 100
		start := time.Now()
		sum = operators.Add(sum, num)
		sample := time.Since(start)
		if err := lH.RecordValue(int64(sample)); err != nil {
			errCount++
		}
	}
	_ = sum

	fmt.Printf("\noperators.Add(), N=%d, since=%v, errCount=%d\n", count, time.Since(start), errCount)
	fmt.Printf("P95 = %v\n", lH.ValueAtPercentile(95))
	fmt.Printf("P99 = %v\n", lH.ValueAtPercentile(99))
	_, _ = lH.PercentilesPrint(os.Stdout, 1, 1000.0)

	sum = 0
	errCount = 0
	lH.Reset()

	start = time.Now()
	for i := 0; i < count; i++ {
		num := rand.Int64N(201) - 100
		start := time.Now()
		sum = operators.Sub(sum, num)
		sample := time.Since(start)
		if err := lH.RecordValue(int64(sample)); err != nil {
			errCount++
		}
	}
	_ = sum

	fmt.Printf("\noperators.Sub(), N=%d, since=%v, errCount=%d\n", count, time.Since(start), errCount)
	fmt.Printf("P95 = %v\n", lH.ValueAtPercentile(95))
	fmt.Printf("P99 = %v\n", lH.ValueAtPercentile(99))
	_, _ = lH.PercentilesPrint(os.Stdout, 1, 1000.0)
}

func mustLoadLibraries() {
	if err := operators.LoadLibraries(
		"./bin/libcalculator.so",
		"./bin/libcalculator_rust.so",
	); err != nil {
		log.Fatal(err)
	}
}
