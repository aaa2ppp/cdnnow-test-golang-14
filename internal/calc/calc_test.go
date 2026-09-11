package calc

import "testing"

func BenchmarkAdd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Add(1, 2)
	}
}

func BenchmarkSub(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Sub(1, 2)
	}
}

var X int64

//go:noinline
func add(a, b int64) int64 {
	x := a + b

	for i := 0; i < 10000; i++ {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
	}
	X = x

	return a + b
}

func BenchmarkGoControlShot(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = add(1, 2)
	}
}
