package calculators

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
	"aaa2ppp/cdnnow-test-golang-14/internal/pools"
)

// defaultRecordDelay максимальная задержка перед записью метрик по умолчанию
const defaultRecordDelay = 100 * time.Millisecond

// defaultBatchSize максимальный размер партии метрик по умолчанию,
// используется если не задан пул
const defaultBatchSize = 1024

type Values struct {
	Sum int64
	Sub int64
}

var ErrOverloaded = errors.New("calculator overloaded")
var ErrStopped = errors.New("calculator stopped")

type Calculator interface {
	Calculate(num int64) error
	Values() Values
}

type AsyncCalculator interface {
	Calculator
	Stop()
}

type dualState struct {
	addOp operators.Op
	subOp operators.Op
	vals  Values
}

func (c *dualState) add(num int64) time.Duration {
	start := time.Now()
	c.vals.Sum = c.addOp.Apply(c.vals.Sum, num)
	return time.Since(start)
}

func (c *dualState) sub(num int64) time.Duration {
	start := time.Now()
	c.vals.Sub = c.subOp.Apply(c.vals.Sub, num)
	return time.Since(start)
}

func (c *dualState) calculate(num int64) metrics.Sample {
	return metrics.Sample{
		AddDuration: c.add(num),
		SubDuration: c.sub(num),
	}
}

// Sync выполняет вычисления синхронно.
// Клиент ждет результата; Sum и Sub согласованы.
type Sync struct {
	dualState
	recordFn func(metrics.Sample)
	lock     chan struct{} // FIFO-мьютекс, cap=1
}

var _ Calculator = &Sync{}

func NewSync(recordFn func(metrics.Sample)) *Sync {
	return &Sync{
		recordFn: recordFn,
		lock:     make(chan struct{}, 1),
		dualState: dualState{
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

type metricRecorder[S any] struct {
	recordFn    func([]S)
	recordDelay time.Duration
	batchPool   *pools.BatchPool[S]
	batchSize   int
	batch       []S
	flushTm     *time.Timer
}

func (c *metricRecorder[S]) makeBatch() []S {
	if c.batchPool != nil {
		return c.batchPool.Get()
	}
	return make([]S, 0, c.batchSize)
}

func (c *metricRecorder[S]) record(sample S) {
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

func (c *metricRecorder[S]) flush() {
	if c.recordFn == nil || c.batch == nil {
		return
	}

	c.recordFn(c.batch)
	c.batch = nil
	c.flushTm.Stop()
}

// Async выполняет вычисления в отдельной горутине.
// Клиент не ждет; Sum и Sub согласованы.
type Async struct {
	dualState
	metricRecorder[metrics.Sample]

	// server
	numCh          chan asyncCmd
	valCh          chan Values
	done           chan struct{}
	ignoreOverload bool
	stopMu         sync.Mutex
	stopped        atomic.Bool
}

var _ AsyncCalculator = &Async{}

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
		dualState: dualState{
			addOp: operators.AddOp(),
			subOp: operators.SubOp(),
		},

		metricRecorder: metricRecorder[metrics.Sample]{
			recordFn:    recordFn,
			recordDelay: defaultRecordDelay,
			batchPool:   batchPool,
			batchSize:   batchSize,
		},

		numCh:          make(chan asyncCmd, queueSize),
		valCh:          make(chan Values),
		done:           make(chan struct{}),
		ignoreOverload: queueSize == 0,
	}

	go c.serve()
	return &c
}

//go:generate enumer -type asyncCmdKind
type asyncCmdKind uint8

const (
	asyncCmdCalc asyncCmdKind = iota
	asyncCmdStop
)

type asyncCmd struct {
	kind asyncCmdKind
	num  int64
}

func (c *Async) serve() {
	defer close(c.done)
	defer c.flush()

	c.flushTm = time.NewTimer(time.Hour)
	c.flushTm.Stop()
	defer c.flushTm.Stop()

	for {
		select {
		case <-c.flushTm.C:
			c.flush()

		case cmd := <-c.numCh:
			switch cmd.kind {
			case asyncCmdCalc:
				sample := c.calculate(cmd.num)
				c.record(sample)
			case asyncCmdStop:
				return
			}

		case c.valCh <- c.vals:
		}
	}
}

// Calculate отправляет число на обработку. Возвращает ошибки:
//   - ErrOverloaded - очередь переполнена
//   - ErrStopped - был вызван метод Stop
func (c *Async) Calculate(num int64) error {
	if c.stopped.Load() {
		return ErrStopped
	}
	cmd := asyncCmd{kind: asyncCmdCalc, num: num}
	if c.ignoreOverload {
		c.numCh <- cmd
		return nil
	}
	select {
	case c.numCh <- cmd:
		return nil
	default:
		return ErrOverloaded
	}
}

// Values возвращает текущие Sum и Sub. После Stop возвращает финальные значения.
func (c *Async) Values() Values {
	select {
	case vals := <-c.valCh:
		return vals
	case <-c.done:
		return c.vals
	}
}

// Stop останавливает калькулятор.
func (c *Async) Stop() {
	c.stopMu.Lock()
	defer c.stopMu.Unlock()

	if c.stopped.Load() {
		<-c.done
		return
	}

	c.stopped.Store(true)
	c.numCh <- asyncCmd{kind: asyncCmdStop}
	<-c.done
}

// asyncSingle аналогично Async, но только для одного оператора.
type asyncSingle struct {
	// single state
	op  operators.Op
	val int64

	metricRecorder[time.Duration]

	// server
	numCh          chan asyncCmd
	valCh          chan int64
	done           chan struct{}
	ignoreOverload bool
	stopMu         sync.Mutex
	stopped        atomic.Bool
}

// newAsyncSingle запускает калькулятор в отдельной горутине для заданного оператора.
func newAsyncSingle(
	op operators.Op,
	queueSize int,
	record func([]time.Duration),
	batchPool *pools.BatchPool[time.Duration],

) *asyncSingle {
	batchSize := defaultBatchSize
	if batchPool != nil {
		batchSize = batchPool.BatchSize()
	}

	c := &asyncSingle{
		op: op,

		metricRecorder: metricRecorder[time.Duration]{
			recordFn:    record,
			recordDelay: defaultRecordDelay,
			batchPool:   batchPool,
			batchSize:   batchSize,
		},

		numCh:          make(chan asyncCmd, queueSize),
		valCh:          make(chan int64),
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

func (c *asyncSingle) serve() {
	defer close(c.done)
	defer c.flush()

	c.flushTm = time.NewTimer(time.Hour)
	c.flushTm.Stop()
	defer c.flushTm.Stop()

	for {
		select {
		case <-c.flushTm.C:
			c.flush()

		case cmd := <-c.numCh:
			switch cmd.kind {
			case asyncCmdCalc:
				sample := c.calculate(cmd.num)
				c.record(sample)
			case asyncCmdStop:
				return
			}

		case c.valCh <- c.val:
		}
	}
}

// Calculate отправляет число на обработку. Возвращает ошибки:
//   - ErrOverloaded - очередь переполнена
//   - ErrStopped - был вызван метод Stop
func (c *asyncSingle) Calculate(num int64) error {
	if c.stopped.Load() {
		return ErrStopped
	}

	cmd := asyncCmd{kind: asyncCmdCalc, num: num}

	if c.ignoreOverload {
		c.numCh <- cmd
		return nil
	}

	select {
	case c.numCh <- cmd:
		return nil
	default:
		return ErrOverloaded
	}
}

// Value возвращает текущее значение. После Stop возвращает финальное значение.
func (c *asyncSingle) Value() int64 {
	select {
	case val := <-c.valCh:
		return val
	case <-c.done:
		return c.val
	}
}

// Stop останавливает калькулятор. Безопасен при повторном вызове.
func (c *asyncSingle) Stop() {
	c.stopMu.Lock()
	defer c.stopMu.Unlock()

	if c.stopped.Load() {
		<-c.done
		return
	}

	c.stopped.Store(true)
	c.numCh <- asyncCmd{kind: asyncCmdStop}
	<-c.done
}

// Parallel выполняет Add и Sub в параллельных горутинах.
// Клиент не ждёт; Sum и Sub в моменте могут расходиться.
type Parallel struct {
	sum *asyncSingle
	sub *asyncSingle
}

var _ AsyncCalculator = &Parallel{}

// NewParallel запускает асинхронный калькулятор, который вычисляет Sum и Sub в параллельных горутинах.
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

	// Для обеспечения согласованности результата, вычисления должны быть выполнены ОБЕИМИ калькуляторами.
	// В случае сбоя в одном из них, результат второго должен быть отброшен.
	// У нас возможно только ErrOverload. SubOp выполняется не медленнее, чем AddOp. Мы будем проверять
	// перегрузку только в Sum калькуляторе, а Sub вычислять безусловно в случае успеха Sum.
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
	// Если Sum завершился успехом, вычисляем Sub безусловно.
	// В конструкторе должна быть отключена проверка перегрузки для Sub: sub.ignoreOverload = true
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
