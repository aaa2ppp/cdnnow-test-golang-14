package clients

import (
	"fmt"
	"net/http"
	"strings"
)

type HTTPError struct {
	Code    int
	Message string
}

func (e *HTTPError) Error() string {
	code := e.Code
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(code)
	}
	return fmt.Sprintf("http %d: %s", code, msg)
}
