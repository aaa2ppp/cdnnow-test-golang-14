package queue

// Stack реализация стека на слайсе.
type Stack[T any] struct {
	buf []T
}

func (s *Stack[T]) Len() int {
	return len(s.buf)
}

func (s *Stack[T]) Empty() bool {
	return len(s.buf) == 0
}

func (s *Stack[T]) Push(v T) {
	s.buf = append(s.buf, v)
}

func (s *Stack[T]) Top() T {
	if s.Empty() {
		panic("stack is empty")
	}
	n := len(s.buf)
	return (s.buf)[n-1]
}

func (s *Stack[T]) Pop() T {
	if s.Empty() {
		panic("stack is empty")
	}
	old := s.buf
	n := len(old)
	v := old[n-1]
	var zero T
	old[n-1] = zero
	s.buf = old[:n-1]
	return v
}

func (s *Stack[T]) Reset() {
	clear(s.buf)
	s.buf = s.buf[:0]
}

// Queue реализация очереди на двух стеках.
type Queue[T any] struct {
	input  Stack[T]
	output Stack[T]
}

func (q *Queue[T]) Empty() bool {
	return q.input.Empty() && q.output.Empty()
}

func (q *Queue[T]) Len() int {
	return q.input.Len() + q.output.Len()
}

func (q *Queue[T]) Push(v T) {
	q.input.Push(v)
}

func (q *Queue[T]) Front() T {
	if q.Empty() {
		panic("queue is empty")
	}
	q.pour()
	return q.output.Top()
}

func (q *Queue[T]) Pop() T {
	if q.Empty() {
		panic("queue is empty")
	}
	q.pour()
	return q.output.Pop()
}

func (q *Queue[T]) pour() {
	if q.output.Empty() {
		for !q.input.Empty() {
			q.output.Push(q.input.Pop())
		}
	}
}

func (q *Queue[T]) Reset() {
	q.input.Reset()
	q.output.Reset()
}

// Deque реализация дека на кольцевом буфере.
type Deque[T any] struct {
	buf   []T
	front int
	size  int
}

func (d *Deque[T]) Len() int { return d.size }

func (d *Deque[T]) Empty() bool { return d.size == 0 }

func (d *Deque[T]) Front() T {
	if d.size == 0 {
		panic("deque: empty")
	}
	return d.buf[d.front]
}

func (d *Deque[T]) Back() T {
	if d.size == 0 {
		panic("deque: empty")
	}
	back := d.front + d.size - 1
	if back >= len(d.buf) {
		back -= len(d.buf)
	}
	return d.buf[back]
}

func (d *Deque[T]) PushBack(v T) {
	if d.size == len(d.buf) {
		d.grow(1)
	}
	back := d.front + d.size
	if back >= len(d.buf) {
		back -= len(d.buf)
	}
	d.buf[back] = v
	d.size++
}

func (d *Deque[T]) PushFront(v T) {
	if d.size == len(d.buf) {
		d.grow(1)
	}
	if d.front == 0 {
		d.front = len(d.buf) - 1
	} else {
		d.front--
	}
	d.buf[d.front] = v
	d.size++
}

func (d *Deque[T]) PopFront() T {
	v := d.Front()
	var zero T
	d.buf[d.front] = zero
	d.front++
	if d.front == len(d.buf) {
		d.front = 0
	}
	d.size--
	return v
}

func (d *Deque[T]) PopBack() T {
	v := d.Back()
	back := d.front + d.size - 1
	if back >= len(d.buf) {
		back -= len(d.buf)
	}
	var zero T
	d.buf[back] = zero
	d.size--
	return v
}

func (d *Deque[T]) At(idx int) T {
	if idx < 0 || idx >= d.size {
		panic("deque: index out of range")
	}
	pos := d.front + idx
	if pos >= len(d.buf) {
		pos -= len(d.buf)
	}
	return d.buf[pos]
}

func (d *Deque[T]) Reset() {
	if d.size == 0 {
		return
	}
	if d.front+d.size <= len(d.buf) {
		clear(d.buf[d.front : d.front+d.size])
	} else {
		clear(d.buf[d.front:])
		clear(d.buf[:d.front+d.size-len(d.buf)])
	}
	d.front = 0
	d.size = 0
}

func (d *Deque[T]) Grow(n int) {
	if n < 0 {
		panic("deque.Grow: negative count")
	}
	if len(d.buf)-d.size < n {
		d.grow(n)
	}
}

// grow ВСЕГДА перевыделяет буфер, увеличивая его ёмкость.
// Вызывать ТОЛЬКО когда точно известно, что текущего свободного места недостаточно.
// Публичный метод Grow содержит необходимую проверку и вызывает grow только при необходимости.
func (d *Deque[T]) grow(need int) {
	if len(d.buf) == 0 {
		d.buf = make([]T, max(need, 16))
		return
	}

	newCap := max(len(d.buf)*2, d.size+need)
	newBuf := make([]T, newCap)

	if d.front+d.size <= len(d.buf) {
		copy(newBuf, d.buf[d.front:d.front+d.size])
	} else {
		n := copy(newBuf, d.buf[d.front:])
		copy(newBuf[n:], d.buf[:d.size-n])
	}

	d.buf = newBuf
	d.front = 0
}

// NewStackFrom создаёт стек из существующего слайса, забирая его владение.
func NewStackFrom[T any](slice []T) *Stack[T] {
	return &Stack[T]{buf: slice}
}

// NewQueueFrom создаёт очередь из существующего слайса, забирая его владение.
func NewQueueFrom[T any](slice []T) *Queue[T] {
	return &Queue[T]{input: Stack[T]{buf: slice}}
}

// NewDequeFrom создаёт дек из существующего слайса, забирая его владение.
func NewDequeFrom[T any](slice []T) *Deque[T] {
	return &Deque[T]{
		buf:   slice,
		front: 0,
		size:  len(slice),
	}
}
