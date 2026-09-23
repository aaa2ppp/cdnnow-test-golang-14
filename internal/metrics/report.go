package metrics

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"
)

type SnapshotGetter interface {
	GetSnapshot() Snapshot
}

type Printer struct {
	stats         SnapshotGetter
	mu            sync.Mutex
	cache         []byte
	cacheDeadline time.Time
}

func NewPrinter(stats SnapshotGetter) *Printer {
	return &Printer{
		stats: stats,
	}
}

func (r *Printer) Print(w io.Writer) error {
	_, err := w.Write(r.getReportText())
	return err
}

func (r *Printer) getReportText() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if !now.Before(r.cacheDeadline) {
		var b bytes.Buffer
		_ = printReport(&b, r.stats.GetSnapshot())
		r.cache = b.Bytes()
		r.cacheDeadline = time.Now().Add(500 * time.Millisecond)
	}
	return r.cache
}

var requestMeta = []struct {
	kind RequestKind
	name string
	help string
}{
	{Ok, "calc_ok_1s", "Successfully handled requests per second"},
	{BadRequest, "calc_bad_request_1s", "Requests rejected due to bad client data per second"},
	{Overload, "calc_overload_1s", "Requests rejected due to overload per second"},
	{Failed, "calc_failed_1s", "Requests failed during execution per second"},
}

var histogramMeta = []struct {
	kind DurationKind
	name string
	help string
}{
	{Add, "calc_c_duration_ns", "C `add` function call duration, nanoseconds"},
	{Sub, "calc_rust_duration_ns", "Rust `sub` function call duration, nanoseconds"},
}

func printReport(b io.Writer, snap Snapshot) error {
	var err error
	printf := func(f string, a ...any) {
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, f, a...)
	}

	// --- RPS за последние 60 секунд ---
	for _, meta := range requestMeta {
		printf("# HELP %s %s, last 60 seconds\n", meta.name, meta.help)
		printf("# TYPE %s gauge\n", meta.name)
		rps := snap.Requests[meta.kind]
		for i := len(rps) - 1; i >= 0; i-- {
			sec := i - (len(rps) - 1)
			val := rps[i]
			printf("%s{second=\"%d\"} %d\n", meta.name, sec, val)
		}
	}

	// --- p95/p99 для C/Rust функций ---
	for _, meta := range histogramMeta {
		printf("# HELP %s %s\n", meta.name, meta.help)
		printf("# TYPE %s gauge\n", meta.name)
		printf("%s{quantile=\"0.95\"} %d\n", meta.name, snap.Histograms[meta.kind].ValueAtPercentile(95))
		printf("%s{quantile=\"0.99\"} %d\n", meta.name, snap.Histograms[meta.kind].ValueAtPercentile(99))
	}

	return err
}
