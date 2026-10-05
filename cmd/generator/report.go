package main

import (
	"fmt"
	"io"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/generator/workers"
)

func PrintReport(w io.Writer, stat *workers.Stat, withPercentiles bool) error {
	if _, err := fmt.Fprintf(w, "Total requests: ok=%d errors=%d\n", stat.Ok, stat.Errors); err != nil {
		return err
	}

	if withPercentiles {
		if count := stat.Overflow.Count(); count > 0 {
			perc := float64(count) * 100 / float64(stat.Ok)
			median := stat.Overflow.Median()
			if _, err := fmt.Fprintf(w, "Overflows: count=%d (%0.3f%%), median=%v\n", count, perc, median); err != nil {
				return err
			}
		}
		if _, err := stat.Hist.PercentilesPrint(w, 1, float64(time.Microsecond)); err != nil {
			return err
		}
	}

	return nil
}
