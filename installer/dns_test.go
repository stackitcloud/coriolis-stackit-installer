package installer

import "testing"

func TestEqualDNSIgnoresCaseAndTrailingDot(t *testing.T) {
	if !equalDNS("Coriolis.Example.test.", "coriolis.example.test") {
		t.Fatal("names should match")
	}
}
