package loggers

import (
	"fmt"
	"log"
	"time"
)

const defaultWindow = 2 * time.Second

type Logger interface {
	Print(a ...any)
	Printf(f string, a ...any)
}

type dedupMsgMeta struct {
	flushAt    time.Time
	suppressed int
}

//go:generate enumer -type dedupCmdKind
type dedupCmdKind uint8

type dedupCmd struct {
	kind dedupCmdKind
	msg  string
}

const (
	dedupCmdLog dedupCmdKind = iota
	dedupCmdClose
)

type Dedup struct {
	logger   Logger
	window   time.Duration
	lastMsgs map[string]dedupMsgMeta
	cmdCh    chan dedupCmd
	done     chan struct{}
}

func NewDedup(logger Logger, queue int, window time.Duration) *Dedup {
	if logger == nil {
		logger = log.Default()
	}
	if window == 0 {
		window = defaultWindow
	}

	lg := &Dedup{
		logger:   logger,
		window:   window,
		lastMsgs: make(map[string]dedupMsgMeta),
		cmdCh:    make(chan dedupCmd, queue),
		done:     make(chan struct{}),
	}

	lg.serve()
	return lg
}

func (lg *Dedup) serve() {
	go func() {
		defer close(lg.done)

		tk := time.NewTicker(lg.window / 4)
		defer tk.Stop()

		for {
			select {
			case cmd := <-lg.cmdCh:
				switch cmd.kind {
				case dedupCmdLog:
					lg.log(cmd.msg)
				case dedupCmdClose:
					lg.forceFlush()
					return
				}
			case <-tk.C:
				lg.maybeFlush()
			}
		}
	}()
}

func (lg *Dedup) send(cmd dedupCmd) {
	select {
	case lg.cmdCh <- cmd:
	case <-lg.done:
	}
}

func (lg *Dedup) Close() {
	lg.send(dedupCmd{kind: dedupCmdClose})
	<-lg.done
}

func (lg *Dedup) Printf(f string, a ...any) {
	lg.send(dedupCmd{
		kind: dedupCmdLog,
		msg:  fmt.Sprintf(f, a...),
	})
}

func (lg *Dedup) log(msg string) {
	if meta, ok := lg.lastMsgs[msg]; ok {
		meta.suppressed++
		lg.lastMsgs[msg] = meta
		return
	}

	lg.logger.Print(msg)
	lg.remember(time.Now(), msg)
}

func (lg *Dedup) printMore(msg string, n int) {
	lg.logger.Printf("%s (+%d more)", msg, n)
}

func (lg *Dedup) remember(at time.Time, msg string) {
	flushAt := at.Add(lg.window)
	lg.lastMsgs[msg] = dedupMsgMeta{flushAt: flushAt}
}

func (lg *Dedup) forceFlush() {
	for msg, meta := range lg.lastMsgs {
		if meta.suppressed > 0 {
			lg.printMore(msg, meta.suppressed)
		}
		delete(lg.lastMsgs, msg)
	}
}

func (lg *Dedup) maybeFlush() {
	now := time.Now()
	for msg, meta := range lg.lastMsgs {
		if now.Sub(meta.flushAt) < 0 {
			continue
		}
		if meta.suppressed > 0 {
			lg.printMore(msg, meta.suppressed)
			lg.remember(now, msg)
		} else {
			delete(lg.lastMsgs, msg)
		}
	}
}
