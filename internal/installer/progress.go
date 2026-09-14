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
	progressActivityMu        sync.Mutex
	progressActivities        []*progressActivity
)

type progressActivity struct {
	ctx     context.Context
	label   string
	started time.Time
	stop    chan struct{}
	done    chan struct{}
	detail  atomic.Value
}

func progressAction(ctx context.Context, label string, action func() error) error {
	activity := startProgressActivity(ctx, label)
	err := action()
	activity.finish(err)
	return err
}

func progressActionWithUpdates(ctx context.Context, label string, action func(update func(string)) error) error {
	activity := startProgressActivity(ctx, label)
	err := action(activity.update)
	activity.finish(err)
	return err
}

// deploymentPhase gives each top-level deployment phase its own timeout. A
// large image import must not consume the time budget required by later server
// bootstrap or certificate phases.
func deploymentPhase(parent context.Context, timeout time.Duration, label string, action func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := progressAction(ctx, label, func() error { return action(ctx) })
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("phase %q exceeded configured timeout %s: %w", label, timeout, err)
	}
	return err
}

func startProgressActivity(ctx context.Context, label string) *progressActivity {
	a := &progressActivity{ctx: ctx, label: label, started: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	progressActivityMu.Lock()
	progressActivities = append(progressActivities, a)
	progressActivityMu.Unlock()
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
			if !a.isForeground() {
				continue
			}
			last := time.Unix(0, lastVisibleProgress.Load())
			if now.Sub(last) >= progressHeartbeatInterval {
				writeProgress("WAIT ", a.labelWithDetail(), now.Sub(a.started))
			}
		}
	}
}

func (a *progressActivity) isForeground() bool {
	progressActivityMu.Lock()
	defer progressActivityMu.Unlock()
	return len(progressActivities) > 0 && progressActivities[len(progressActivities)-1] == a
}

func (a *progressActivity) update(detail string) {
	if detail == "" {
		return
	}
	if previous, ok := a.detail.Load().(string); ok && previous == detail {
		return
	}
	a.detail.Store(detail)
	writeProgress("INFO ", a.labelWithDetail(), time.Since(a.started))
}

func (a *progressActivity) labelWithDetail() string {
	if detail, ok := a.detail.Load().(string); ok && detail != "" {
		return a.label + ": " + detail
	}
	return a.label
}

func (a *progressActivity) finish(err error) {
	close(a.stop)
	<-a.done
	state := "DONE "
	if err != nil {
		state = "FAIL "
	}
	writeProgress(state, a.label, time.Since(a.started))
	progressActivityMu.Lock()
	for i := len(progressActivities) - 1; i >= 0; i-- {
		if progressActivities[i] == a {
			progressActivities = append(progressActivities[:i], progressActivities[i+1:]...)
			break
		}
	}
	progressActivityMu.Unlock()
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

func writeProgressOutput(output string) {
	if output == "" {
		return
	}
	markVisibleProgress()
	progressOutputMu.Lock()
	defer progressOutputMu.Unlock()
	fmt.Fprint(progressOutput, output)
}

func writeInfo(format string, args ...any) {
	writeStatus("[INFO ] "+format+"\n", args...)
}

func writeWarning(format string, args ...any) {
	writeStatus("[WARN ] "+format+"\n", args...)
}

func formatProgressDuration(elapsed time.Duration) string {
	if elapsed < time.Second {
		return elapsed.Round(10 * time.Millisecond).String()
	}
	return elapsed.Round(time.Second).String()
}
