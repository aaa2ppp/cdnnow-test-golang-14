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
			"Sync",
			func() Calculator {
				return NewSync(nil)
			},
		},
		{
			"Async",
			func() Calculator {
				c := NewAsync(0, nil, nil)
				return c
			},
		},
		{
			"Async q10",
			func() Calculator {
				c := NewAsync(10, nil, nil)
				return c
			},
		},
		{
			"Parallel",
			func() Calculator {
				c := NewParallel(0, nil, nil)
				return c
			},
		},
		{
			"Parallel q10",
			func() Calculator {
				c := NewParallel(10, nil, nil)
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

func BenchmarkSync(b *testing.B) {
	calc := NewSync(nil)
	for i := 0; i < b.N; i++ {
		_ = calc.Calculate(42)
	}
}

func BenchmarkAsync(b *testing.B) {
	c := NewAsync(1024, nil, nil)
	c.ignoreOverload = true
	for i := 0; i < b.N; i++ {
		_ = c.Calculate(42)
	}
	c.Stop()
}

func BenchmarkAsyncSingle(b *testing.B) {
	cases := []struct {
		name string
		op   operators.Op
	}{
		{
			"Add C lib",
			operators.AddOp(),
		},
		{
			"Sub Rust lib",
			operators.SubOp(),
		},
	}

	for _, cs := range cases {
		b.Run(cs.name, func(b *testing.B) {
			c := newAsyncSingle(cs.op, 1024, nil, nil)
			c.ignoreOverload = true
			for i := 0; i < b.N; i++ {
				_ = c.Calculate(42)
			}
			c.Stop()
		})
	}
}

func BenchmarkParallel(b *testing.B) {
	c := NewParallel(1024, nil, nil)
	c.sum.ignoreOverload = true
	c.sub.ignoreOverload = true

	for i := 0; i < b.N; i++ {
		_ = c.Calculate(42)
	}
	c.Stop()
}
