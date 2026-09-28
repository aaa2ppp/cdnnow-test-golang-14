package operators

import (
	"log"
	"testing"

	"github.com/aaa2ppp/be"
)

const (
	cLibPath    = "../../bin/libcalculator.so"
	rustLibPath = "../../bin/libcalculator_rust.so"
)

func init() {
	err := LoadLibraries(cLibPath, rustLibPath)
	if err != nil {
		log.Fatal(err)
	}
}

func TestOp(t *testing.T) {
	tests := []struct {
		name string
		op   Op
		a, b int64
		want int64
	}{
		{
			"Add",
			AddOp(),
			1, 2,
			3,
		},
		{
			"Sub",
			SubOp(),
			1, 2,
			-1,
		},
		{
			"stub",
			stubOp(), // (a + b) под капотом
			1, 2,
			3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.op.Apply(tt.a, tt.b)
			be.Equal(t, got, tt.want)
		})
	}
}

func BenchmarkOp(b *testing.B) {
	tests := []struct {
		name string
		op   Op
	}{
		{
			"Add C lib",
			AddOp(),
		},
		{
			"Sub Rust lib",
			SubOp(),
		},
		{
			"stub",
			stubOp(),
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = tt.op.Apply(1, 2)
			}
		})
	}
}

var X int64

//go:noinline
func workload(a, b int64) int64 {
	x := a + b

	for i := 0; i < 10000; i++ {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
	}
	X = x

	return a + b
}

// для сравнения
func BenchmarkWorkload_Go(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = workload(1, 2)
	}
}
