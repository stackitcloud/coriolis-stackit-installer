package installer

import "testing"

func TestCommandOutputDeltaGrowingBuffer(t *testing.T) {
	delta, reset := commandOutputDelta("first\n", "first\nsecond\n")
	if reset || delta != "second\n" {
		t.Fatalf("delta=%q reset=%t", delta, reset)
	}
}

func TestCommandOutputDeltaRollingWindow(t *testing.T) {
	common := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ--"
	previous := "discarded-prefix\n" + common + "old-tail\n"
	current := common + "old-tail\nnew-tail\n"
	delta, reset := commandOutputDelta(previous, current)
	if reset || delta != "new-tail\n" {
		t.Fatalf("delta=%q reset=%t", delta, reset)
	}
}

func TestCommandOutputDeltaShortOverlap(t *testing.T) {
	delta, reset := commandOutputDelta("progress 90%\r", "90%\rprogress 100%\n")
	if reset || delta != "progress 100%\n" {
		t.Fatalf("delta=%q reset=%t", delta, reset)
	}
}

func TestCommandOutputDeltaReportsReset(t *testing.T) {
	delta, reset := commandOutputDelta("unrelated old output", "completely new output")
	if !reset || delta != "completely new output" {
		t.Fatalf("delta=%q reset=%t", delta, reset)
	}
}
