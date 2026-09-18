package installer

import (
	"reflect"
	"testing"
)

func TestSplitCommaList(t *testing.T) {
	got := splitCommaList(" project-a,project-b, ,project-c ")
	want := []string{"project-a", "project-b", "project-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCommaList()=%v, want %v", got, want)
	}
}
