package calculators

import (
	"log"
	"testing"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/operators"

	"github.com/aaa2ppp/be"
)

func init() {
	if err := operators.LoadLibraries(
		"../../bin/libcalculator.so",
		"../../bin/libcalculator_rust.so",
	); err != nil {
		log.Fatal(err)
	}
}

func TestCalculators(t *testing.T) {
	tests := []struct {
		name    string
		newCalc func() Calculator
	}{
		{
			"sync calc",
			func() Calculator {
				return NewSyncCalculator(nil)
			},
		},
		{
			"async calc",
			func() Calculator {
				c := NewAsyncCalculator(1024, nil, nil)
				c.IgnoreOverload()
				return c
			},
		},
		{
			"parallel calc",
			func() Calculator {
				c := NewParallelCalculator(1024, nil, nil)
				c.IgnoreOverload()
				return c
			},
		},
	}

	type stopper interface{ Stop() }

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := tt.newCalc()
			if calc, ok := calc.(stopper); ok {
				defer calc.Stop()
			}
			for _, num := range []int64{3, 4, 5, 6, 7, 8, 9} {
				err := calc.Calculate(num)
				be.Err(t, err, nil)
			}
			time.Sleep(10 * time.Millisecond)
			vals := calc.Values()
			be.Equal(t, vals.Sum, 42)
			be.Equal(t, vals.Sub, -42)
		})
	}
}

func BenchmarkSyncCalc(b *testing.B) {
	calc := NewSyncCalculator(nil)
	for i := 0; i < b.N; i++ {
		_ = calc.Calculate(42)
	}
}

func BenchmarkAsyncCalc(b *testing.B) {
	c := NewAsyncCalculator(1024, nil, nil)
	c.IgnoreOverload()
	for i := 0; i < b.N; i++ {
		_ = c.Calculate(42)
	}
	c.Stop()
}

func BenchmarkSingle(b *testing.B) {
	cases := []struct {
		name   string
		calcFn CalculateFunc
	}{
		{
			"C calc",
			operators.Add,
		},
		{
			"Rust calc",
			operators.Sub,
		},
	}

	for _, cs := range cases {
		b.Run(cs.name, func(b *testing.B) {
			c := newSingleAsyncCalculator(cs.calcFn, 1024, nil, nil)
			c.IgnoreOverload()
			for i := 0; i < b.N; i++ {
				_ = c.Calculate(42)
			}
			c.Stop()
		})
	}
}

func BenchmarkParallelCalc(b *testing.B) {
	c := NewParallelCalculator(1024, nil, nil)
	c.IgnoreOverload()
	for i := 0; i < b.N; i++ {
		_ = c.Calculate(42)
	}
	c.Stop()
}
