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

func (r *Printer) Print(w io.Writer) (int64, error) {
	n, err := w.Write(r.getReportText())
	return int64(n), err
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

func printReport(b io.Writer, snap Snapshot) error {
	var err error
	printf := func(f string, a ...any) {
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, f, a...)
	}

	// --- RPS за последние 60 секунд ---
	printf("# HELP calc_rps_1s Requests per second, last 60 seconds\n")
	printf("# TYPE calc_rps_1s gauge\n")
	for i := len(snap.RPS) - 1; i >= 0; i-- {
		sec := i - (len(snap.RPS) - 1)
		val := snap.RPS[i]
		printf("calc_rps_1s{second=\"%d\"} %d\n", sec, val)
	}

	// --- p95/p99 для C ---
	printf("# HELP calc_c_duration_ns C `add` function call duration, nanoseconds\n")
	printf("# TYPE calc_c_duration_ns gauge\n")
	printf("calc_c_duration_ns{quantile=\"0.95\"} %d\n", snap.AddHist.ValueAtPercentile(95))
	printf("calc_c_duration_ns{quantile=\"0.99\"} %d\n", snap.AddHist.ValueAtPercentile(99))

	// --- p95/p99 для Rust ---
	printf("# HELP calc_rust_duration_ns Rust `sub` function call duration, nanoseconds\n")
	printf("# TYPE calc_rust_duration_ns gauge\n")
	printf("calc_rust_duration_ns{quantile=\"0.95\"} %d\n", snap.SubHist.ValueAtPercentile(95))
	printf("calc_rust_duration_ns{quantile=\"0.99\"} %d\n", snap.SubHist.ValueAtPercentile(99))

	return err
}
