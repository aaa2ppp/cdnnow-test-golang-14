package workers

import (
	"math"
	"math/bits"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

const histLowestDiscernible = 1 * time.Microsecond
const histHighestTrackable = 100 * time.Millisecond
const histSignificantDigits = 3

type HistOverflow struct {
	count   int64
	sumLog2 int64
}

// Для оценки абсолютная точность не требуется
func log2u(x uint64) int64 {
	return int64(bits.Len64(x) - 1)
}

func (ho *HistOverflow) add(x time.Duration) {
	ho.count++
	ho.sumLog2 += log2u(uint64(x))
}

func (ho *HistOverflow) Count() int64 {
	return ho.count
}

// При Count=0 не имеет смысла, возвращает 0
func (ho *HistOverflow) Median() time.Duration {
	if ho.count == 0 {
		return 0
	}
	meanLog2 := float64(ho.sumLog2) / float64(ho.count)
	return time.Duration(math.Round(math.Exp2(meanLog2)))
}

type Stat struct {
	Ok       int64
	Errors   int64
	Hist     *hdrhistogram.Histogram
	Overflow HistOverflow
}

func NewStat() *Stat {
	return &Stat{
		Hist: hdrhistogram.New(
			int64(histLowestDiscernible),
			int64(histHighestTrackable),
			histSignificantDigits,
		),
	}
}

func (s *Stat) Merge(other *Stat) {
	if other != nil {
		s.Ok += other.Ok
		s.Errors += other.Errors
		s.Hist.Merge(other.Hist)
		s.Overflow.count += other.Overflow.count
		s.Overflow.sumLog2 += other.Overflow.sumLog2
	}
}
