package api

import (
	"net/http"
	"strconv"
)

type Service interface {
	Process(num int64)
}

func New(svc Service) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /calc", calcHandler(svc))
	return mux
}

func calcHandler(c Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		numStr := q.Get("num")
		if numStr == "" {
			http.Error(w, "missing 'num' query parameter", 400)
			return
		}
		num, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			http.Error(w, "'num' must be an integer", 400)
			return
		}
		c.Process(num)
		_, _ = w.Write([]byte("ok"))
	}
}
