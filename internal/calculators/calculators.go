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

type state struct {
	addOp operators.Op
	subOp operators.Op
	vals  Values
}

func (c *state) add(num int64) time.Duration {
	start := time.Now()
	c.vals.Sum = c.addOp.Apply(c.vals.Sum, num)
	return time.Since(start)
}

func (c *state) sub(num int64) time.Duration {
	start := time.Now()
	c.vals.Sub = c.subOp.Apply(c.vals.Sub, num)
	return time.Since(start)
}

func (c *state) calculate(num int64) metrics.Sample {
	return metrics.Sample{
		AddDuration: c.add(num),
		SubDuration: c.sub(num),
	}
}

// Sync выполняет вычисления синхронно.
// Клиент ждет результата; Sum и Sub согласованы.
type Sync struct {
	state
	recordFn func(metrics.Sample)
	lock     chan struct{} // FIFO-мьютекс, cap=1
}

var _ Calculator = &Sync{}

func NewSync(recordFn func(metrics.Sample)) *Sync {
	return &Sync{
		recordFn: recordFn,
		lock:     make(chan struct{}, 1),
		state: state{
			addOp: operators.AddOp(),
			subOp: operators.SubOp(),
		},
	}
}

func (c *Sync) record(sample metrics.Sample) {
	if c.recordFn != nil {
		c.recordFn(sample)
	}
}

func (c *Sync) Calculate(num int64) error {
	c.lock <- struct{}{}
	sample := c.calculate(num)
	<-c.lock
	c.record(sample)
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
	state

	// metrics recorder
	recordFn    func([]metrics.Sample)
	recordDelay time.Duration
	batchPool   *pools.BatchPool[metrics.Sample]
	batchSize   int
	batch       []metrics.Sample
	flushTm     *time.Timer

	// server
	numCh          chan int64
	valCh          chan Values
	done           chan struct{}
	ignoreOverload bool
}

var _ Calculator = &Async{}

// NewAsync запускает калькулятор в отдельной горутине.
func NewAsync(
	queueSize int,
	recordFn func([]metrics.Sample),
	batchPool *pools.BatchPool[metrics.Sample],

) *Async {
	batchSize := defaultBatchSize
	if batchPool != nil {
		batchSize = batchPool.BatchSize()
	}

	c := Async{
		state: state{
			addOp: operators.AddOp(),
			subOp: operators.SubOp(),
		},

		recordFn:    recordFn,
		recordDelay: defaultRecordingDelay,
		batchPool:   batchPool,
		batchSize:   batchSize,

		numCh:          make(chan int64, queueSize),
		valCh:          make(chan Values),
		done:           make(chan struct{}),
		ignoreOverload: queueSize == 0,
	}

	go c.serve()
	return &c
}

func (c *Async) makeBatch() []metrics.Sample {
	if c.batchPool != nil {
		return c.batchPool.Get()
	}
	return make([]metrics.Sample, 0, c.batchSize)
}

func (c *Async) record(sample metrics.Sample) {
	if c.recordFn == nil {
		return
	}

	if c.batch == nil {
		c.batch = c.makeBatch()
		c.flushTm.Reset(c.recordDelay)
	}

	c.batch = append(c.batch, sample)
	if len(c.batch) >= c.batchSize {
		c.flush()
	}
}

func (c *Async) flush() {
	if c.recordFn == nil || c.batch == nil {
		return
	}

	c.recordFn(c.batch)
	c.batch = nil
	c.flushTm.Stop()
}

func (c *Async) serve() {
	defer func() {
		close(c.valCh)
		close(c.done)
	}()

	defer c.flush()

	c.flushTm = time.NewTimer(time.Hour)
	c.flushTm.Stop()
	defer c.flushTm.Stop()

	for {
		select {
		case <-c.flushTm.C:
			c.flush()

		case num, ok := <-c.numCh:
			if !ok {
				return
			}
			sample := c.calculate(num)
			c.record(sample)

		case c.valCh <- c.vals:
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
	if vals, ok := <-c.valCh; ok {
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
	// state
	op  operators.Op
	val int64

	// metric recorder
	recordFn    func([]time.Duration)
	recordDelay time.Duration
	batch       []time.Duration
	batchPool   *pools.BatchPool[time.Duration]
	batchSize   int
	flushTm     *time.Timer

	// server
	numCh          chan int64
	valCh          chan int64
	done           chan struct{}
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
		op:       op,
		recordFn: record,

		recordDelay: defaultRecordingDelay,
		batchPool:   pool,
		batchSize:   batchSize,

		numCh: make(chan int64, queueSize),
		valCh: make(chan int64),
		done:  make(chan struct{}),

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
	if c.batchPool != nil {
		return c.batchPool.Get()
	}
	return make([]time.Duration, 0, c.batchSize)
}

func (c *asyncSingle) record(sample time.Duration) {
	if c.recordFn == nil {
		return
	}

	if c.batch == nil {
		c.batch = c.makeBatch()
		c.flushTm.Reset(c.recordDelay)
	}

	c.batch = append(c.batch, sample)
	if len(c.batch) >= c.batchSize {
		c.flush()
	}
}

func (c *asyncSingle) flush() {
	if c.recordFn == nil || c.batch == nil {
		return
	}

	c.recordFn(c.batch)
	c.batch = nil
	c.flushTm.Stop()
}

func (c *asyncSingle) serve() {
	defer func() {
		close(c.valCh)
		close(c.done)
	}()

	defer c.flush()

	c.flushTm = time.NewTimer(time.Hour)
	c.flushTm.Stop()
	defer c.flushTm.Stop()

	for {
		select {
		case <-c.flushTm.C:
			c.flush()

		case num, ok := <-c.numCh:
			if !ok {
				return
			}
			sample := c.calculate(num)
			c.record(sample)

		case c.valCh <- c.val:
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
	if val, ok := <-c.valCh; ok {
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
