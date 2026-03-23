package utils

type FIFOList[T any] struct {
	items []T
}

// NewFIFOList creates a new FIFO list
func NewFIFOList[T any]() *FIFOList[T] {
	return &FIFOList[T]{
		items: make([]T, 0),
	}
}

// Push adds an item to the end of the list
func (f *FIFOList[T]) Push(item T) {
	f.items = append(f.items, item)
}

// Pop removes and returns the first item from the list
// Returns the zero value of T and false if the list is empty
func (f *FIFOList[T]) Pop() (T, bool) {
	if len(f.items) == 0 {
		var zero T
		return zero, false
	}
	item := f.items[0]
	f.items = f.items[1:]
	return item, true
}

// Peek returns the first item without removing it
// Returns the zero value of T and false if the list is empty
func (f *FIFOList[T]) Peek() (T, bool) {
	if len(f.items) == 0 {
		var zero T
		return zero, false
	}
	return f.items[0], true
}

// Len returns the number of items in the list
func (f *FIFOList[T]) Len() int {
	return len(f.items)
}

// IsEmpty returns true if the list is empty
func (f *FIFOList[T]) IsEmpty() bool {
	return len(f.items) == 0
}

// Clear removes all items from the list
func (f *FIFOList[T]) Clear() {
	f.items = make([]T, 0)
}
