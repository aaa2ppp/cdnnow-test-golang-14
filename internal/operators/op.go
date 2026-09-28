//go:build cgo

package operators

// NOTE: purego не взяли, т.к экономия 20-40ns на вызове на фоне 15–50µs операции - шум

/*
#include <stdint.h>

typedef int64_t (*operator_fn)(int64_t, int64_t);

int64_t stub_fn(int64_t a, int64_t b) { return a + b; }

static inline int64_t apply_operator(operator_fn fn, int64_t a, int64_t b) {
    return fn(a, b);
}
*/
import "C"

type operatorFn C.operator_fn

type Op struct {
	fn operatorFn
}

func (op Op) Apply(a, b int64) int64 {
	return int64(C.apply_operator(op.fn, C.int64_t(a), C.int64_t(b)))
}

// FOR TEST ONLY просто (a + b)
func stubOp() Op { return Op{operatorFn(C.stub_fn)} }
