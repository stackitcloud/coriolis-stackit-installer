package installer

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProgressActionReportsHeartbeatAndCompletion(t *testing.T) {
	oldOutput, oldInterval := progressOutput, progressHeartbeatInterval
	defer func() {
		progressOutput = oldOutput
		progressHeartbeatInterval = oldInterval
	}()
	var output bytes.Buffer
	progressOutput = &output
	progressHeartbeatInterval = 5 * time.Millisecond

	err := progressAction(context.Background(), "testing operation", func() error {
		time.Sleep(18 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"[START] testing operation", "[WAIT ] testing operation", "[DONE ] testing operation"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("progress output %q does not contain %q", output.String(), expected)
		}
	}
}

func TestProgressActionReportsFailure(t *testing.T) {
	oldOutput := progressOutput
	defer func() { progressOutput = oldOutput }()
	var output bytes.Buffer
	progressOutput = &output
	want := errors.New("failed")
	if err := progressAction(context.Background(), "failing operation", func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("error=%v, want %v", err, want)
	}
	if !strings.Contains(output.String(), "[FAIL ] failing operation") {
		t.Fatalf("unexpected progress output %q", output.String())
	}
}

func TestProgressActionSuppressesHeartbeatDuringVisibleProgress(t *testing.T) {
	oldOutput, oldInterval := progressOutput, progressHeartbeatInterval
	defer func() {
		progressOutput = oldOutput
		progressHeartbeatInterval = oldInterval
	}()
	var output bytes.Buffer
	progressOutput = &output
	progressHeartbeatInterval = 5 * time.Millisecond

	if err := progressAction(context.Background(), "active transfer", func() error {
		for range 5 {
			time.Sleep(3 * time.Millisecond)
			markVisibleProgress()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "[WAIT ]") {
		t.Fatalf("unexpected heartbeat during visible progress: %q", output.String())
	}
}

func TestProgressActionReportsAndRepeatsCurrentDetail(t *testing.T) {
	oldOutput, oldInterval := progressOutput, progressHeartbeatInterval
	defer func() {
		progressOutput = oldOutput
		progressHeartbeatInterval = oldInterval
	}()
	var output bytes.Buffer
	progressOutput = &output
	progressHeartbeatInterval = 5 * time.Millisecond

	err := progressActionWithUpdates(context.Background(), "waiting for resource", func(update func(string)) error {
		update("current status CREATING")
		update("current status CREATING")
		time.Sleep(18 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Count(got, "[INFO ] waiting for resource: current status CREATING") != 1 {
		t.Fatalf("detail update should be emitted once, got %q", got)
	}
	if !strings.Contains(got, "[WAIT ] waiting for resource: current status CREATING") {
		t.Fatalf("heartbeat does not include current detail: %q", got)
	}
}

func TestNestedProgressOnlyHeartbeatsForMostSpecificActivity(t *testing.T) {
	oldOutput, oldInterval := progressOutput, progressHeartbeatInterval
	defer func() {
		progressOutput = oldOutput
		progressHeartbeatInterval = oldInterval
	}()
	var output bytes.Buffer
	progressOutput = &output
	progressHeartbeatInterval = 5 * time.Millisecond

	err := progressAction(context.Background(), "outer operation", func() error {
		return progressAction(context.Background(), "specific inner operation", func() error {
			time.Sleep(18 * time.Millisecond)
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "[WAIT ] outer operation") {
		t.Fatalf("outer heartbeat obscures the active inner phase: %q", got)
	}
	if !strings.Contains(got, "[WAIT ] specific inner operation") {
		t.Fatalf("specific inner heartbeat is missing: %q", got)
	}
}

func TestDeploymentPhaseHasIndependentDescriptiveTimeout(t *testing.T) {
	err := deploymentPhase(context.Background(), 5*time.Millisecond, "slow phase", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want deadline exceeded", err)
	}
	for _, expected := range []string{`phase "slow phase"`, "configured timeout 5ms"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not contain %q", err, expected)
		}
	}
}

func TestDeploymentPhasesReceiveFreshTimeoutBudgets(t *testing.T) {
	started := time.Now()
	for _, label := range []string{"image phase", "bootstrap phase"} {
		if err := deploymentPhase(context.Background(), 50*time.Millisecond, label, func(context.Context) error {
			time.Sleep(30 * time.Millisecond)
			return nil
		}); err != nil {
			t.Fatalf("%s unexpectedly failed: %v", label, err)
		}
	}
	if time.Since(started) <= 50*time.Millisecond {
		t.Fatal("test did not exceed one phase budget in total")
	}
}
