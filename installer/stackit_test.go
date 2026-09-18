package installer

import (
	"strings"
	"testing"
)

func TestContainsString(t *testing.T) {
	if !containsString([]string{"one", "two"}, "two") {
		t.Fatal("expected value to be found")
	}
	if containsString([]string{"one", "two"}, "three") {
		t.Fatal("unexpected value found")
	}
}

func TestUniqueStrings(t *testing.T) {
	got := uniqueStrings([]string{"b", "a", "b", "", "a"})
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("uniqueStrings()=%v", got)
	}
}
