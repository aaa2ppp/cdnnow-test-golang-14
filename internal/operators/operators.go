//go:build linux

package operators

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
#include <stdint.h>

typedef int64_t (*operator_fn)(int64_t, int64_t);

static inline int64_t call_operator(operator_fn fn, int64_t a, int64_t b) {
    return fn(a, b);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

type operators struct {
	load      sync.Once
	stickyErr error
	add       C.operator_fn
	sub       C.operator_fn
}

func (c *operators) loadLibs(cLibPath, rustLibPath string) error {
	c.load.Do(func() {
		cLib, err := loadLib(cLibPath)
		if err != nil {
			c.stickyErr = fmt.Errorf("operators: %w", err)
			return
		}

		add, err := getSymbol(cLib, "add")
		if err != nil {
			c.stickyErr = fmt.Errorf("operators: %w", err)
			return
		}

		rustLib, err := loadLib(rustLibPath)
		if err != nil {
			c.stickyErr = fmt.Errorf("operators: %w", err)
			return
		}

		sub, err := getSymbol(rustLib, "sub")
		if err != nil {
			c.stickyErr = fmt.Errorf("operators: %w", err)
			return
		}

		c.add = C.operator_fn(add)
		c.sub = C.operator_fn(sub)
	})
	return c.stickyErr
}

func loadLib(path string) (unsafe.Pointer, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	handle := C.dlopen(cPath, C.RTLD_NOW)
	if handle == nil {
		return nil, errors.New(C.GoString(C.dlerror()))
	}

	return handle, nil
}

func getSymbol(handle unsafe.Pointer, name string) (unsafe.Pointer, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	C.dlerror() // Очищаем старые ошибки
	sym := C.dlsym(handle, cName)

	if sym == nil {
		errStr := C.dlerror()
		if errStr != nil {
			return nil, errors.New(C.GoString(errStr))
		}
		return nil, fmt.Errorf("symbol '%s' not found", name)
	}

	return sym, nil
}

var ops operators

func LoadLibraries(cLibPath, rustLibPath string) error {
	return ops.loadLibs(cLibPath, rustLibPath)
}

func Add(a, b int64) int64 {
	if ops.add == nil {
		panic("operators: libraries not loaded")
	}
	return int64(C.call_operator(ops.add, C.int64_t(a), C.int64_t(b)))
}

func Sub(a, b int64) int64 {
	if ops.sub == nil {
		panic("operators: libraries not loaded")
	}
	return int64(C.call_operator(ops.sub, C.int64_t(a), C.int64_t(b)))
}
