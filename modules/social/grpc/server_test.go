package grpc

import "testing"

func TestNewServerReturnsDistinctInstances(t *testing.T) {
	first := NewServer()
	second := NewServer()
	if first == nil || second == nil {
		t.Fatal("NewServer returned nil instance")
	}
	if first == second {
		t.Fatal("NewServer returned shared instance")
	}
}
