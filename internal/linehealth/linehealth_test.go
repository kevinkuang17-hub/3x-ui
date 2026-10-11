package linehealth

import (
	"slices"
	"testing"
)

func TestSetReportsTransitionsOnly(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	if !Set(3, true) {
		t.Fatal("first down must be a transition")
	}
	if Set(3, true) {
		t.Fatal("repeated down must not be a transition")
	}
	if !IsDown(3) || IsDown(4) {
		t.Fatal("IsDown mismatch")
	}
	Set(1, true)
	if got := Down(); !slices.Equal(got, []int{1, 3}) {
		t.Fatalf("Down() = %v", got)
	}
	if !Set(3, false) || IsDown(3) {
		t.Fatal("up must clear the mark")
	}
	if Set(3, false) {
		t.Fatal("repeated up must not be a transition")
	}
}
