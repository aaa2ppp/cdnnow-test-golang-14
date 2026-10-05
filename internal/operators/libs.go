//go:build cgo && linux

package operators

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
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
	addFn     operatorFn
	subFn     operatorFn
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

		c.addFn = operatorFn(add)
		c.subFn = operatorFn(sub)
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

func mustLoaded(fn operatorFn) operatorFn {
	if fn == nil {
		panic("operators: libraries not loaded")
	}
	return fn
}

func AddOp() Op { return Op{mustLoaded(ops.addFn)} }
func SubOp() Op { return Op{mustLoaded(ops.subFn)} }

// Deprecated: use AddOp().Apply(a, b)
func Add(a, b int64) int64 { return AddOp().Apply(a, b) }

// Deprecated: use SubOp().Apply(a, b)
func Sub(a, b int64) int64 { return SubOp().Apply(a, b) }
