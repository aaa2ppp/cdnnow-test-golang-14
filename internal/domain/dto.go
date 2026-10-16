package domain

import (
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

type Values struct {
	Sum int64
	Sub int64
}

type Sample struct {
	Add time.Duration
	Sub time.Duration
}

//go:generate enumer -type RequestKind
type RequestKind uint8

const (
	Ok RequestKind = iota
	Overload
	BadRequest
	Failed

	RequestKindSize // держать последним
)

//go:generate enumer -type DurationKind
type DurationKind uint8

const (
	Add DurationKind = iota
	Sub

	DurationKindSize // держать последним
)

type Snapshot struct {
	Version    uint64
	Requests   [RequestKindSize][]int64
	Histograms [DurationKindSize]*hdrhistogram.Histogram
}
