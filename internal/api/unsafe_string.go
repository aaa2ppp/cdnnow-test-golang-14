package api

import "unsafe"

func unsafeString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
