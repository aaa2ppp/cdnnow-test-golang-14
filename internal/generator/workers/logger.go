package workers

import (
	"log"
	"time"
)

const defaultErrorWindow = 2 * time.Second

type errorCount struct {
	flushAt    time.Time
	suppressed int
}

type errorLogger struct {
	ID          int
	ErrorWindow time.Duration
	lastErrors  map[string]errorCount
}

func (lg *errorLogger) logError(err error) {
	msg := err.Error()
	now := time.Now()

	cnt, ok := lg.lastErrors[msg]
	if ok && now.Sub(cnt.flushAt) < 0 {
		cnt.suppressed++
		lg.lastErrors[msg] = cnt
		return
	}

	lg.print(msg, cnt.suppressed)
}

func (lg *errorLogger) print(msg string, suppressed int) {
	if suppressed == 0 {
		log.Printf("[worker %d] request failed: %s", lg.ID, msg)
	} else {
		log.Printf("[worker %d] request failed: %s (+%d similar)", lg.ID, msg, suppressed)
	}
	lg.remember(msg)
}

func (lg *errorLogger) remember(msg string) {
	if lg.lastErrors == nil {
		lg.lastErrors = map[string]errorCount{}
	}
	window := lg.ErrorWindow
	if window == 0 {
		window = defaultErrorWindow
	}
	flashAt := time.Now().Add(window)
	lg.lastErrors[msg] = errorCount{flushAt: flashAt, suppressed: 0}
}

func (lg *errorLogger) maybeFlush(force bool) {
	now := time.Now()
	for msg, cnt := range lg.lastErrors {
		if cnt.suppressed > 0 && (force || now.Sub(cnt.flushAt) >= 0) {
			lg.print(msg, cnt.suppressed-1)
		}
		if cnt.suppressed == 0 && now.Sub(cnt.flushAt) >= 0 {
			delete(lg.lastErrors, msg)
		}
	}
}
