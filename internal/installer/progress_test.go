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
