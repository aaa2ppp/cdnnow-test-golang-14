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

// Sync выполняет вычисления синхронно.
// Клиент ждет результата; Sum и Sub согласованы.
type Sync struct {
	addOp    operators.Op
	subOp    operators.Op
	recordFn func(metrics.Sample)
	lock     chan struct{} // FIFO-мьютекс, cap=1

	vals Values
}

var _ Calculator = &Sync{}

func NewSync(recordFn func(metrics.Sample)) *Sync {
	return &Sync{
		addOp:    operators.AddOp(),
		subOp:    operators.SubOp(),
		recordFn: recordFn,
		lock:     make(chan struct{}, 1),
	}
}

func (c *Sync) add(num int64) time.Duration {
	start := time.Now()
	c.vals.Sum = c.addOp.Apply(c.vals.Sum, num)
	return time.Since(start)
}

func (c *Sync) sub(num int64) time.Duration {
	start := time.Now()
	c.vals.Sub = c.subOp.Apply(c.vals.Sub, num)
	return time.Since(start)
}

func (c *Sync) calculate(num int64) metrics.Sample {
	return metrics.Sample{
		AddDuration: c.add(num),
		SubDuration: c.sub(num),
	}
}

func (c *Sync) Calculate(num int64) error {
	c.lock <- struct{}{}
	sample := c.calculate(num)
	<-c.lock

	if c.recordFn != nil {
		c.recordFn(sample)
	}

	return nil
}

func (c *Sync) Values() Values {
	c.lock <- struct{}{}
	vals := c.vals
	<-c.lock
	return vals
}

// Async выполняет вычисления в отдельной горутине.
// Клиент не ждет; Sum и Sub согласованы.
type Async struct {
	addOp     operators.Op
	subOp     operators.Op
	vals      Values
	recordFn  func([]metrics.Sample)
	pool      *pools.BatchPool[metrics.Sample]
	batchSize int
	recDelay  time.Duration
	numCh     chan int64
	getValCh  chan Values
	done      chan struct{}

	ignoreOverload bool
}

var _ Calculator = &Async{}

// NewAsync запускает калькулятор в отдельной горутине.
func NewAsync(
	queueSize int,
	recordFn func([]metrics.Sample),
	pool *pools.BatchPool[metrics.Sample],

) *Async {
	batchSize := defaultBatchSize
	if pool != nil {
		batchSize = pool.BatchSize()
	}

	c := Async{
		addOp:          operators.AddOp(),
		subOp:          operators.SubOp(),
		recordFn:       recordFn,
		pool:           pool,
		batchSize:      batchSize,
		recDelay:       defaultRecordingDelay,
		numCh:          make(chan int64, queueSize),
		getValCh:       make(chan Values),
		done:           make(chan struct{}),
		ignoreOverload: queueSize == 0,
	}

	go c.serve()
	return &c
}

func (c *Async) add(num int64) time.Duration {
	start := time.Now()
	c.vals.Sum = c.addOp.Apply(c.vals.Sum, num)
	return time.Since(start)
}

func (c *Async) sub(num int64) time.Duration {
	start := time.Now()
	c.vals.Sub = c.subOp.Apply(c.vals.Sub, num)
	return time.Since(start)
}

func (c *Async) calculate(num int64) metrics.Sample {
	return metrics.Sample{
		AddDuration: c.add(num),
		SubDuration: c.sub(num),
	}
}

func (c *Async) makeBatch() []metrics.Sample {
	if c.pool != nil {
		return c.pool.Get()
	}
	return make([]metrics.Sample, 0, c.batchSize)
}

func (c *Async) serve() {
	defer func() {
		close(c.getValCh)
		close(c.done)
	}()

	var batch []metrics.Sample
	defer func() {
		if c.recordFn != nil && batch != nil {
			c.recordFn(batch)
		}
	}()

	flushTm := time.NewTimer(time.Hour)
	flushTm.Stop()
	defer flushTm.Stop()

	for {
		select {
		case <-flushTm.C:
			c.recordFn(batch)
			batch = nil

		case num, ok := <-c.numCh:
			if !ok {
				return
			}

			sample := c.calculate(num)

			if c.recordFn != nil {
				if batch == nil {
					batch = c.makeBatch()
					flushTm.Reset(c.recDelay)
				}

				batch = append(batch, sample)

				if len(batch) >= c.batchSize {
					c.recordFn(batch)
					batch = nil
					flushTm.Stop()
				}
			}

		case c.getValCh <- c.vals:
		}
	}
}

// Calculate отправляет число на обработку. Паникует после Stop.
func (c *Async) Calculate(num int64) error {
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
func (c *Async) Values() Values {
	if vals, ok := <-c.getValCh; ok {
		return vals
	}
	return c.vals
}

// Stop останавливает калькулятор. Паникует при повторном вызове.
func (c *Async) Stop() {
	close(c.numCh)
	<-c.done
}

// asyncSingle аналогично Async, но только для одной операции.
type asyncSingle struct {
	op        operators.Op
	val       int64
	record    func([]time.Duration)
	pool      *pools.BatchPool[time.Duration]
	batchSize int
	recDelay  time.Duration
	numCh     chan int64
	getValCh  chan int64
	done      chan struct{}

	ignoreOverload bool
}

// newAsyncSingle запускает калькулятор в отдельной горутине для заданной операции.
func newAsyncSingle(
	op operators.Op,
	queueSize int,
	record func([]time.Duration),
	pool *pools.BatchPool[time.Duration],

) *asyncSingle {
	batchSize := defaultBatchSize
	if pool != nil {
		batchSize = pool.BatchSize()
	}

	c := &asyncSingle{
		op:             op,
		record:         record,
		pool:           pool,
		batchSize:      batchSize,
		recDelay:       defaultRecordingDelay,
		numCh:          make(chan int64, queueSize),
		getValCh:       make(chan int64),
		done:           make(chan struct{}),
		ignoreOverload: queueSize == 0,
	}

	go c.serve()
	return c
}

func (c *asyncSingle) calculate(num int64) time.Duration {
	start := time.Now()
	c.val = c.op.Apply(c.val, num)
	return time.Since(start)
}

func (c *asyncSingle) makeBatch() []time.Duration {
	if c.pool != nil {
		return c.pool.Get()
	}
	return make([]time.Duration, 0, c.batchSize)
}

func (c *asyncSingle) serve() {
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

// Calculate отправляет число на обработку. Паникует после Stop.
func (c *asyncSingle) Calculate(num int64) error {
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
func (c *asyncSingle) Value() int64 {
	if val, ok := <-c.getValCh; ok {
		return val
	}
	return c.val
}

// Stop останавливает калькулятор. Паникует при повторном вызове.
func (c *asyncSingle) Stop() {
	close(c.numCh)
	<-c.done
}

// Parallel выполняет Add и Sub в параллельных горутинах.
// Клиент не ждёт; Sum и Sub в моменте могут расходиться.
type Parallel struct {
	sum *asyncSingle
	sub *asyncSingle
}

var _ Calculator = &Parallel{}

// NewParallel запускает асинхронный калькулятор, который вычисляет Add и Sub в параллельных горутинах.
func NewParallel(
	queueSize int,
	recordFn func(metrics.DurationKind, []time.Duration),
	pool *pools.BatchPool[time.Duration],

) *Parallel {
	var recordAdd, recordSub func([]time.Duration)
	if recordFn != nil {
		recordAdd = func(d []time.Duration) { recordFn(metrics.Add, d) }
		recordSub = func(d []time.Duration) { recordFn(metrics.Sub, d) }
	}

	sum := newAsyncSingle(operators.AddOp(), queueSize, recordAdd, pool)
	sub := newAsyncSingle(operators.SubOp(), queueSize, recordSub, pool)

	// Для обеспечения согласованности результата, вычисления должны быть выполнены ОБЕИМИ функциями.
	// В случае сбоя в одной из них, результат второй должен быть отброшен.
	// У нас возможно только ErrOverload. Add выполняется медленнее, чем Sub. Мы будем проверять
	// перегрузку только в addCalc, а Sub выполнять безусловно в случае успеха Add.
	// Если вы измените это поведение, вам необходимо внести соответствующие изменения в метод Calculate.
	//
	// Примечание: в редких случаях на это может привести к блокировке горутины HTTP-обработчика,
	// что является осознанным компромиссом ради сохранения согласованности без сложного отката.
	sub.ignoreOverload = true

	return &Parallel{
		sum: sum,
		sub: sub,
	}
}

func (c *Parallel) Calculate(num int64) error {
	if err := c.sum.Calculate(num); err != nil {
		return err
	}
	// Если Add завершился успехом, выполняем Sub безусловно.
	// В конструкторе должна быть отключена проверка перегрузки для Sub: subCalc.IgnoreOverload()
	_ = c.sub.Calculate(num)
	return nil
}

func (c *Parallel) Values() Values {
	return Values{
		Sum: c.sum.Value(),
		Sub: c.sub.Value(),
	}
}

func (c *Parallel) Stop() {
	c.sum.Stop()
	c.sub.Stop()
}
