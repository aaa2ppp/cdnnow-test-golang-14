package calculators

import (
	"errors"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
	"aaa2ppp/cdnnow-test-golang-14/internal/pools"
)

// defaultRecordingDelay максимальная задержка перед записью метрик по умолчанию
const defaultRecordingDelay = 100 * time.Millisecond

// defaultBatchSize максимальный размер партии метрик по умолчанию,
// используется если не задан пул
const defaultBatchSize = 1024

type Values struct {
	Sum int64
	Sub int64
}

var ErrOverloaded = errors.New("calculator overloaded")

type Calculator interface {
	Calculate(num int64) error
	Values() Values
}

// SyncCalculator простой синхронный калькулятор. Клиент ждет завершения вычислений.
type SyncCalculator struct {
	record func(metrics.Sample)
	lock   chan struct{} // FIFO-мьютекс, cap=1

	vals Values
}

var _ Calculator = &SyncCalculator{}

func NewSyncCalculator(record func(metrics.Sample)) *SyncCalculator {
	return &SyncCalculator{
		record: record,
		lock:   make(chan struct{}, 1),
	}
}

func (c *SyncCalculator) add(num int64) time.Duration {
	start := time.Now()
	c.vals.Sum = operators.Add(c.vals.Sum, num)
	return time.Since(start)
}

func (c *SyncCalculator) sub(num int64) time.Duration {
	start := time.Now()
	c.vals.Sub = operators.Sub(c.vals.Sub, num)
	return time.Since(start)
}

func (c *SyncCalculator) calculate(num int64) metrics.Sample {
	return metrics.Sample{
		AddDuration: c.add(num),
		SubDuration: c.sub(num),
	}
}

func (c *SyncCalculator) Calculate(num int64) error {
	c.lock <- struct{}{}
	sample := c.calculate(num)
	<-c.lock

	if c.record != nil {
		c.record(sample)
	}

	return nil
}

func (c *SyncCalculator) Values() Values {
	c.lock <- struct{}{}
	vals := c.vals
	<-c.lock
	return vals
}

// AsyncCalculator выполняет вычисления в отдельной горутине. Вычисления производятся в фоне.
// Синхронизация через канал.
type AsyncCalculator struct {
	vals      Values
	record    func([]metrics.Sample)
	pool      *pools.BatchPool[metrics.Sample]
	batchSize int
	recDelay  time.Duration
	numCh     chan int64
	getValCh  chan Values
	done      chan struct{}

	ignoreOverload bool
}

var _ Calculator = &AsyncCalculator{}

// NewAsyncCalculator запускает калькулятор в отдельной горутине.
func NewAsyncCalculator(
	queueSize int,
	record func([]metrics.Sample),
	pool *pools.BatchPool[metrics.Sample],

) *AsyncCalculator {
	batchSize := defaultBatchSize
	if pool != nil {
		batchSize = pool.BatchSize()
	}

	c := AsyncCalculator{
		record:    record,
		pool:      pool,
		batchSize: batchSize,
		recDelay:  defaultRecordingDelay,
		numCh:     make(chan int64, queueSize),
		getValCh:  make(chan Values),
		done:      make(chan struct{}),
	}

	go c.serve()
	return &c
}

func (c *AsyncCalculator) add(num int64) time.Duration {
	start := time.Now()
	c.vals.Sum = operators.Add(c.vals.Sum, num)
	return time.Since(start)
}

func (c *AsyncCalculator) sub(num int64) time.Duration {
	start := time.Now()
	c.vals.Sub = operators.Sub(c.vals.Sub, num)
	return time.Since(start)
}

func (c *AsyncCalculator) calculate(num int64) metrics.Sample {
	return metrics.Sample{
		AddDuration: c.add(num),
		SubDuration: c.sub(num),
	}
}

func (c *AsyncCalculator) makeBatch() []metrics.Sample {
	if c.pool != nil {
		return c.pool.Get()
	}
	return make([]metrics.Sample, 0, c.batchSize)
}

func (c *AsyncCalculator) serve() {
	defer func() {
		close(c.getValCh)
		close(c.done)
	}()

	var batch []metrics.Sample
	defer func() {
		if c.record != nil && batch != nil {
			c.record(batch)
		}
	}()

	flushTm := time.NewTimer(time.Hour)
	flushTm.Stop()
	defer flushTm.Stop()

	for {
		select {
		case <-flushTm.C:
			c.record(batch)
			batch = nil

		case num, ok := <-c.numCh:
			if !ok {
				return
			}

			sample := c.calculate(num)

			if c.record != nil {
				if batch == nil {
					batch = c.makeBatch()
					flushTm.Reset(c.recDelay)
				}

				batch = append(batch, sample)

				if len(batch) >= c.batchSize {
					c.record(batch)
					batch = nil
					flushTm.Stop()
				}
			}

		case c.getValCh <- c.vals:
		}
	}
}

// IgnoreOverload FOR TEST ONLY
func (c *AsyncCalculator) IgnoreOverload() {
	c.ignoreOverload = true
}

// Calculate отправляет число на обработку. Паникует после Stop.
func (c *AsyncCalculator) Calculate(num int64) error {
	if c.ignoreOverload {
		c.numCh <- num
		return nil
	}
	select {
	case c.numCh <- num:
		return nil
	default:
		return ErrOverloaded
	}
}

// Values возвращает текущие Sum и Sub. После Stop возвращает финальные значения.
func (c *AsyncCalculator) Values() Values {
	if vals, ok := <-c.getValCh; ok {
		return vals
	}
	return c.vals
}

// Stop останавливает калькулятор. Паникует при повторном вызове.
func (c *AsyncCalculator) Stop() {
	close(c.numCh)
	<-c.done
}

type CalculateFunc func(a, b int64) int64

// asyncSingleCalculator аналогично СhannelCalculator, но только для одной операции.
type asyncSingleCalculator struct {
	val       int64
	calcFn    CalculateFunc
	record    func([]time.Duration)
	pool      *pools.BatchPool[time.Duration]
	batchSize int
	recDelay  time.Duration
	numCh     chan int64
	getValCh  chan int64
	done      chan struct{}

	ignoreOverload bool
}

// newSingleAsyncCalculator запускает калькулятор в отдельной горутине для заданной операции.
func newSingleAsyncCalculator(
	calcFn CalculateFunc,
	queueSize int,
	record func([]time.Duration),
	pool *pools.BatchPool[time.Duration],

) *asyncSingleCalculator {
	batchSize := defaultBatchSize
	if pool != nil {
		batchSize = pool.BatchSize()
	}

	c := &asyncSingleCalculator{
		calcFn:    calcFn,
		record:    record,
		pool:      pool,
		batchSize: batchSize,
		recDelay:  defaultRecordingDelay,
		numCh:     make(chan int64, queueSize),
		getValCh:  make(chan int64),
		done:      make(chan struct{}),
	}

	go c.serve()
	return c
}

func (c *asyncSingleCalculator) calculate(num int64) time.Duration {
	start := time.Now()
	c.val = c.calcFn(c.val, num)
	return time.Since(start)
}

func (c *asyncSingleCalculator) makeBatch() []time.Duration {
	if c.pool != nil {
		return c.pool.Get()
	}
	return make([]time.Duration, 0, c.batchSize)
}

func (c *asyncSingleCalculator) serve() {
	defer func() {
		close(c.getValCh)
		close(c.done)
	}()

	var batch []time.Duration
	defer func() {
		if c.record != nil && batch != nil {
			c.record(batch)
		}
	}()

	flushTm := time.NewTimer(time.Hour)
	flushTm.Stop()
	defer flushTm.Stop()

	for {
		select {
		case <-flushTm.C:
			c.record(batch)
			batch = nil

		case num, ok := <-c.numCh:
			if !ok {
				return
			}

			d := c.calculate(num)

			if c.record != nil {
				if batch == nil {
					batch = c.makeBatch()
					flushTm.Reset(c.recDelay)
				}

				batch = append(batch, d)

				if len(batch) >= c.batchSize {
					c.record(batch)
					batch = nil
					flushTm.Stop()
				}
			}

		case c.getValCh <- c.val:
		}
	}
}

// IgnoreOverload FOR TEST ONLY
func (c *asyncSingleCalculator) IgnoreOverload() {
	c.ignoreOverload = true
}

// Calculate отправляет число на обработку. Паникует после Stop.
func (c *asyncSingleCalculator) Calculate(num int64) error {
	if c.ignoreOverload {
		c.numCh <- num
		return nil
	}

	select {
	case c.numCh <- num:
		return nil
	default:
		return ErrOverloaded
	}
}

// Value возвращает текущее значение. После Stop возвращает финальное значение.
func (c *asyncSingleCalculator) Value() int64 {
	if val, ok := <-c.getValCh; ok {
		return val
	}
	return c.val
}

// Stop останавливает калькулятор. Паникует при повторном вызове.
func (c *asyncSingleCalculator) Stop() {
	close(c.numCh)
	<-c.done
}

// ParallelCalculator вычисляет Sum и Sub в параллельных горутинах.
type ParallelCalculator struct {
	addCalc *asyncSingleCalculator
	subCalc *asyncSingleCalculator
}

var _ Calculator = &ParallelCalculator{}

type MetricsAggregator interface {
	RecordAddDurations([]time.Duration)
	RecordSubDurations([]time.Duration)
}

// NewParallelCalculator запускает асинхронный калькулятор, который вычисляет Add и Sub в параллельных горутинах.
func NewParallelCalculator(
	queueSize int,
	aggregator MetricsAggregator,
	pool *pools.BatchPool[time.Duration],

) *ParallelCalculator {
	var recordAdd, recordSub func([]time.Duration)
	if aggregator != nil {
		recordAdd = aggregator.RecordAddDurations
		recordSub = aggregator.RecordSubDurations
	}

	addCalc := newSingleAsyncCalculator(operators.Add, queueSize, recordAdd, pool)
	subCalc := newSingleAsyncCalculator(operators.Sub, queueSize, recordSub, pool)

	// Для обеспечения согласованности результата, вычисления должны быть выполнены ОБЕИМИ функциями.
	// В случае сбоя в одной из них, результат второй должен быть отброшен.
	// У нас возможно только ErrOverload. Add выполняется медленнее, чем Sub. Мы будем проверять
	// перегрузку только в addCalc, а Sub выполнять безусловно в случае успеха Add.
	// Если вы измените это поведение, вам необходимо внести соответствующие изменения в метод Calculate.
	//
	// Примечание: в редких случаях на грани это может привести к блокировке горутины HTTP-обработчика,
	// что является осознанным компромиссом ради сохранения согласованности без сложного отката.
	subCalc.IgnoreOverload()

	return &ParallelCalculator{
		addCalc: addCalc,
		subCalc: subCalc,
	}
}

// IgnoreOverload FOR TEST ONLY
func (c *ParallelCalculator) IgnoreOverload() {
	c.addCalc.IgnoreOverload()
	c.subCalc.IgnoreOverload()
}

func (c *ParallelCalculator) Calculate(num int64) error {
	if err := c.addCalc.Calculate(num); err != nil {
		return err
	}
	// Если Add завершился успехом, выполняем Sub безусловно.
	// В конструкторе должна быть отключена проверка перегрузки для Sub: subCalc.IgnoreOverload()
	_ = c.subCalc.Calculate(num)
	return nil
}

func (c *ParallelCalculator) Values() Values {
	return Values{
		Sum: c.addCalc.Value(),
		Sub: c.subCalc.Value(),
	}
}

func (c *ParallelCalculator) Stop() {
	c.addCalc.Stop()
	c.subCalc.Stop()
}
