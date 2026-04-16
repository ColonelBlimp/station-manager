package utils

import "testing"

func TestFIFOList_Basic(t *testing.T) {
	fifo := NewFIFOList[int]()
	if !fifo.IsEmpty() {
		t.Error("New FIFO should be empty")
	}
	if fifo.Len() != 0 {
		t.Errorf("New FIFO Len() = %d, want 0", fifo.Len())
	}
	_, ok := fifo.Pop()
	if ok {
		t.Error("Pop on empty FIFO should return false")
	}
	_, ok = fifo.Peek()
	if ok {
		t.Error("Peek on empty FIFO should return false")
	}
}
func TestFIFOList_PushPop(t *testing.T) {
	fifo := NewFIFOList[string]()
	fifo.Push("first")
	fifo.Push("second")
	fifo.Push("third")
	if fifo.Len() != 3 {
		t.Errorf("Len() = %d, want 3", fifo.Len())
	}
	if fifo.IsEmpty() {
		t.Error("FIFO with items should not be empty")
	}
	val, ok := fifo.Pop()
	if !ok || val != "first" {
		t.Errorf("Pop() = %q, %v, want first, true", val, ok)
	}
	val, ok = fifo.Pop()
	if !ok || val != "second" {
		t.Errorf("Pop() = %q, %v, want second, true", val, ok)
	}
	val, ok = fifo.Pop()
	if !ok || val != "third" {
		t.Errorf("Pop() = %q, %v, want third, true", val, ok)
	}
	if !fifo.IsEmpty() {
		t.Error("FIFO should be empty after popping all items")
	}
}
func TestFIFOList_Peek(t *testing.T) {
	fifo := NewFIFOList[int]()
	fifo.Push(42)
	fifo.Push(100)
	val, ok := fifo.Peek()
	if !ok || val != 42 {
		t.Errorf("Peek() = %d, %v, want 42, true", val, ok)
	}
	if fifo.Len() != 2 {
		t.Errorf("Len() after Peek = %d, want 2", fifo.Len())
	}
}
func TestFIFOList_Clear(t *testing.T) {
	fifo := NewFIFOList[int]()
	fifo.Push(1)
	fifo.Push(2)
	fifo.Push(3)
	fifo.Clear()
	if !fifo.IsEmpty() {
		t.Error("FIFO should be empty after Clear")
	}
	if fifo.Len() != 0 {
		t.Errorf("Len() after Clear = %d, want 0", fifo.Len())
	}
}
