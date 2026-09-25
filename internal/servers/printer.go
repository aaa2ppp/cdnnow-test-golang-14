package servers

import (
	"log"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
)

type valuer interface {
	Values() calculators.Values
}

func startPeriodicPrinter(calc valuer, interval time.Duration) func() {
	done := make(chan struct{})
	stopCh := make(chan struct{})
	go func() {
		defer close(done)
		tk := time.NewTicker(interval)
		defer tk.Stop()
		for {
			select {
			case <-stopCh:
				printTotals("final", calc)
				return
			case <-tk.C:
				printTotals("periodic", calc)
			}
		}
	}()
	return func() {
		close(stopCh)
		<-done
	}
}

func printTotals(label string, calc valuer) {
	vals := calc.Values()
	log.Printf("[%s] sum=%d sub=%d\n", label, vals.Sum, vals.Sub)
}
