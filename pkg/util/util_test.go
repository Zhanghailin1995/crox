package util

import "testing"

func TestRandomId(t *testing.T) {
	id := RandomId()
	if len(id) != 12 {
		t.Errorf("RandomId() = %v, want %v", id, 12)
	}
	println(id)
}
