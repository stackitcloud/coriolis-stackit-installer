package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var (
	progressHeartbeatInterval           = 20 * time.Second
	progressOutput            io.Writer = os.Stderr
	progressOutputMu          sync.Mutex
	lastVisibleProgress       atomic.Int64
)

type progressActivity struct {
	ctx     context.Context
	label   string
	started time.Time
	stop    chan struct{}
	done    chan struct{}
}

func progressAction(ctx context.Context, label string, action func() error) error {
	activity := startProgressActivity(ctx, label)
	err := action()
	activity.finish(err)
	return err
}

func startProgressActivity(ctx context.Context, label string) *progressActivity {
	a := &progressActivity{ctx: ctx, label: label, started: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	writeProgress("START", label, 0)
	go a.heartbeat()
	return a
}

func (a *progressActivity) heartbeat() {
	defer close(a.done)
	ticker := time.NewTicker(progressHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.stop:
			return
		case now := <-ticker.C:
			last := time.Unix(0, lastVisibleProgress.Load())
			if now.Sub(last) >= progressHeartbeatInterval {
				writeProgress("WAIT ", a.label, now.Sub(a.started))
			}
		}
	}
}

func (a *progressActivity) finish(err error) {
	close(a.stop)
	<-a.done
	state := "DONE "
	if err != nil {
		state = "FAIL "
	}
	writeProgress(state, a.label, time.Since(a.started))
}

func writeProgress(state, label string, elapsed time.Duration) {
	lastVisibleProgress.Store(time.Now().UnixNano())
	progressOutputMu.Lock()
	defer progressOutputMu.Unlock()
	if elapsed <= 0 {
		fmt.Fprintf(progressOutput, "[%s] %s\n", state, label)
		return
	}
	fmt.Fprintf(progressOutput, "[%s] %s (elapsed %s)\n", state, label, formatProgressDuration(elapsed))
}

func markVisibleProgress() {
	lastVisibleProgress.Store(time.Now().UnixNano())
}

func writeStatus(format string, args ...any) {
	markVisibleProgress()
	progressOutputMu.Lock()
	defer progressOutputMu.Unlock()
	fmt.Fprintf(progressOutput, format, args...)
}

func formatProgressDuration(elapsed time.Duration) string {
	if elapsed < time.Second {
		return elapsed.Round(10 * time.Millisecond).String()
	}
	return elapsed.Round(time.Second).String()
}
