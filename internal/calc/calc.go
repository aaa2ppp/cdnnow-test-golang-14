package calc

/*
#cgo LDFLAGS: ${SRCDIR}/../../lib/libcalculator.so
#cgo LDFLAGS: ${SRCDIR}/../../lib/libcalculator_rust.so
#cgo LDFLAGS: -Wl,-rpath,$ORIGIN/../lib
#include "../../c_lib/calculator.h"
#include "../../rust_lib/calculator_rust.h"
*/
import "C"

func Add(a, b int64) int64 { return int64(C.add(C.int64_t(a), C.int64_t(b))) }
func Sub(a, b int64) int64 { return int64(C.sub(C.int64_t(a), C.int64_t(b))) }

// func Add(a, b int64) int64 { return a + b }
// func Sub(a, b int64) int64 { return a - b }
