package calculator

import (
	"sync"

	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
)

type Calculator interface {
	Process(num int64)
	Values() (sum, sub int64)
}

type MutextCalculator struct {
	mu  sync.Mutex
	sum int64
	sub int64
}

func (c *MutextCalculator) Process(num int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sum = operators.Add(c.sum, num)
	c.sub = operators.Sub(c.sub, num)
}

func (c *MutextCalculator) Values() (sum, sub int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sum, c.sub
}

type channelCalculator struct {
	numCh chan int64
	valCh chan [2]int64
	done  chan struct{}
	sum   int64
	sub   int64
}

// NewChannelCalculator запускает калькулятор в отдельной горутине.
// stop останавливает ее и ждет завершения. Должен вызываться только один раз,
// после чего Process использовать нельзя.
func NewChannelCalculator() (_ *channelCalculator, stop func()) {
	c := channelCalculator{
		numCh: make(chan int64),
		valCh: make(chan [2]int64),
		done:  make(chan struct{}),
	}
	stop = func() {
		close(c.numCh) // сигнал остановки
		<-c.done       // ждем выхода горутины
	}
	go c.serve()
	return &c, stop
}

// serve читает числа, обновляет сумму и разность, отдает снимки.
// Завершается по закрытию numCh, закрывая valCh.
func (c *channelCalculator) serve() {
	defer close(c.done)
	defer close(c.valCh)
	for {
		select {
		case num, ok := <-c.numCh:
			if !ok {
				return
			}
			c.sum = operators.Add(c.sum, num)
			c.sub = operators.Sub(c.sub, num)
		case c.valCh <- [2]int64{c.sum, c.sub}:
		}
	}
}

// Process отправляет число на обработку.
// Паникует, если вызван после stop или конкурентно с ним.
func (c *channelCalculator) Process(num int64) {
	c.numCh <- num
}

// Values возвращает текущие sum и sub.
// После stop возвращает финальные значения.
func (c *channelCalculator) Values() (sum, sub int64) {
	if vals, ok := <-c.valCh; ok {
		return vals[0], vals[1]
	}
	return c.sum, c.sub
}
