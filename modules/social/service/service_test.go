package service

import "testing"

func TestNewReturnsDistinctInstances(t *testing.T) {
	first := New()
	second := New()
	if first == nil || second == nil {
		t.Fatal("New returned nil instance")
	}
	if first == second {
		t.Fatal("New returned shared instance")
	}
}
