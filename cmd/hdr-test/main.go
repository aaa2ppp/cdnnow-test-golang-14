package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/operators"

	"github.com/HdrHistogram/hdrhistogram-go"
)

func main() {
	mustLoadLibraries()
	const N = 100_000
	const MaxValue = int64(time.Millisecond)

	var sum int64
	var errCount int
	lH := hdrhistogram.New(1, MaxValue, 3)

	for i := 0; i < N; i++ {
		num := rand.Int64N(201) - 100
		start := time.Now()
		sum = operators.Add(sum, num)
		sample := time.Since(start)
		if err := lH.RecordValue(int64(sample)); err != nil {
			errCount++
		}
	}
	_ = sum

	fmt.Printf("\noperators.Add(), N=%d, errCount=%d\n", N, errCount)
	fmt.Printf("P95 = %v\n", lH.ValueAtPercentile(95))
	fmt.Printf("P99 = %v\n", lH.ValueAtPercentile(99))
	_, _ = lH.PercentilesPrint(os.Stdout, 1, 1000.0)

	sum = 0
	errCount = 0
	lH.Reset()

	for i := 0; i < N; i++ {
		num := rand.Int64N(201) - 100
		start := time.Now()
		sum = operators.Sub(sum, num)
		sample := time.Since(start)
		if err := lH.RecordValue(int64(sample)); err != nil {
			errCount++
		}
	}
	_ = sum

	fmt.Printf("\noperators.Sub(), N=%d, errCount=%d\n", N, errCount)
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
