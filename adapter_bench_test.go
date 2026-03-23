package adapters

import (
	"fmt"
	"github.com/goccy/go-json"
	"testing"

	"github.com/aarondl/null/v8"
)

// Benchmark structs
type BenchSource struct {
	ID          int
	Name        string
	Email       string
	Age         int
	Address     string
	City        string
	State       string
	Zip         string
	Phone       string
	Active      bool
	Score       float64
	Rating      float32
	Description string
}

type BenchDest struct {
	ID          int
	Name        string
	Email       string
	Age         int
	Address     string
	City        string
	State       string
	Zip         string
	Phone       string
	Active      bool
	Score       float64
	Rating      float32
	Description string
}

type BenchSourceWithAdditional struct {
	ID             int
	Name           string
	AdditionalData null.JSON
}

type BenchDestWithAdditional struct {
	ID             int
	Name           string
	Email          string
	Age            int
	Address        string
	City           string
	State          string
	Zip            string
	Phone          string
	Active         bool
	AdditionalData null.JSON
}

func BenchmarkAdapter_BasicFieldCopy(b *testing.B) {
	adapter := New()

	src := &BenchSource{
		ID:          1,
		Name:        "John Doe",
		Email:       "john@example.com",
		Age:         30,
		Address:     "123 Main St",
		City:        "Boston",
		State:       "MA",
		Zip:         "02101",
		Phone:       "555-1234",
		Active:      true,
		Score:       95.5,
		Rating:      4.8,
		Description: "A sample user for benchmarking purposes with a longer description field",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dst := &BenchDest{}
		_ = adapter.Into(dst, src)
	}
}

func BenchmarkAdapter_WithConverter(b *testing.B) {
	adapter := New()

	// Register a converter
	adapter.RegisterConverter("Score", func(src interface{}) (interface{}, error) {
		score, ok := src.(float64)
		if !ok {
			return nil, fmt.Errorf("expected float64")
		}
		return score * 1.1, nil
	})

	src := &BenchSource{
		ID:          1,
		Name:        "John Doe",
		Email:       "john@example.com",
		Age:         30,
		Address:     "123 Main St",
		City:        "Boston",
		State:       "MA",
		Zip:         "02101",
		Phone:       "555-1234",
		Active:      true,
		Score:       95.5,
		Rating:      4.8,
		Description: "A sample user for benchmarking purposes",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dst := &BenchDest{}
		_ = adapter.Into(dst, src)
	}
}

func BenchmarkAdapter_MarshalToAdditionalData(b *testing.B) {
	adapter := New()

	src := &BenchSource{
		ID:          1,
		Name:        "John Doe",
		Email:       "john@example.com",
		Age:         30,
		Address:     "123 Main St",
		City:        "Boston",
		State:       "MA",
		Zip:         "02101",
		Phone:       "555-1234",
		Active:      true,
		Score:       95.5,
		Rating:      4.8,
		Description: "A sample user for benchmarking purposes",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dst := &BenchDestWithAdditional{}
		_ = adapter.Into(dst, src)
	}
}

func BenchmarkAdapter_UnmarshalFromAdditionalData(b *testing.B) {
	adapter := New()

	additionalFields := map[string]interface{}{
		"Email":       "john@example.com",
		"Age":         30,
		"Address":     "123 Main St",
		"City":        "Boston",
		"State":       "MA",
		"Zip":         "02101",
		"Phone":       "555-1234",
		"Active":      true,
		"Description": "A sample user for benchmarking purposes",
	}
	jsonData, _ := json.Marshal(additionalFields)

	src := &BenchSourceWithAdditional{
		ID:             1,
		Name:           "John Doe",
		AdditionalData: null.JSONFrom(jsonData),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dst := &BenchDestWithAdditional{}
		_ = adapter.Into(dst, src)
	}
}

func BenchmarkAdapter_RoundTrip(b *testing.B) {
	adapter := New()

	src := &BenchSource{
		ID:          1,
		Name:        "John Doe",
		Email:       "john@example.com",
		Age:         30,
		Address:     "123 Main St",
		City:        "Boston",
		State:       "MA",
		Zip:         "02101",
		Phone:       "555-1234",
		Active:      true,
		Score:       95.5,
		Rating:      4.8,
		Description: "A sample user for benchmarking purposes",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Step 1: Marshal to AdditionalData
		intermediate := &BenchDestWithAdditional{}
		_ = adapter.Into(intermediate, src)

		// Step 2: Unmarshal from AdditionalData
		dst := &BenchDest{}
		_ = adapter.Into(dst, intermediate)
	}
}

func BenchmarkAdapter_Concurrent(b *testing.B) {
	adapter := New()

	src := &BenchSource{
		ID:          1,
		Name:        "John Doe",
		Email:       "john@example.com",
		Age:         30,
		Address:     "123 Main St",
		City:        "Boston",
		State:       "MA",
		Zip:         "02101",
		Phone:       "555-1234",
		Active:      true,
		Score:       95.5,
		Rating:      4.8,
		Description: "A sample user for benchmarking purposes",
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			dst := &BenchDest{}
			_ = adapter.Into(dst, src)
		}
	})
}

func BenchmarkAdapter_WithMultipleConverters(b *testing.B) {
	adapter := New()

	// Register multiple converters
	adapter.RegisterConverter("Score", func(src interface{}) (interface{}, error) {
		score, ok := src.(float64)
		if !ok {
			return nil, fmt.Errorf("expected float64")
		}
		return score * 1.1, nil
	})

	adapter.RegisterConverter("Rating", func(src interface{}) (interface{}, error) {
		rating, ok := src.(float32)
		if !ok {
			return nil, fmt.Errorf("expected float32")
		}
		return rating * 1.05, nil
	})

	adapter.RegisterConverter("Age", func(src interface{}) (interface{}, error) {
		age, ok := src.(int)
		if !ok {
			return nil, fmt.Errorf("expected int")
		}
		return age + 1, nil
	})

	src := &BenchSource{
		ID:          1,
		Name:        "John Doe",
		Email:       "john@example.com",
		Age:         30,
		Address:     "123 Main St",
		City:        "Boston",
		State:       "MA",
		Zip:         "02101",
		Phone:       "555-1234",
		Active:      true,
		Score:       95.5,
		Rating:      4.8,
		Description: "A sample user for benchmarking purposes",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dst := &BenchDest{}
		_ = adapter.Into(dst, src)
	}
}

// Benchmark memory allocation patterns
func BenchmarkAdapter_SmallStruct(b *testing.B) {
	adapter := New()

	type SmallSource struct {
		ID   int
		Name string
	}

	type SmallDest struct {
		ID   int
		Name string
	}

	src := &SmallSource{ID: 1, Name: "Test"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dst := &SmallDest{}
		_ = adapter.Into(dst, src)
	}
}

func BenchmarkAdapter_LargeStruct(b *testing.B) {
	adapter := New()

	type LargeSource struct {
		F1, F2, F3, F4, F5      string
		F6, F7, F8, F9, F10     string
		F11, F12, F13, F14, F15 int
		F16, F17, F18, F19, F20 int
		F21, F22, F23, F24, F25 float64
		F26, F27, F28, F29, F30 float64
		F31, F32, F33, F34, F35 bool
		F36, F37, F38, F39, F40 bool
		F41, F42, F43, F44, F45 string
		F46, F47, F48, F49, F50 string
	}

	type LargeDest struct {
		F1, F2, F3, F4, F5      string
		F6, F7, F8, F9, F10     string
		F11, F12, F13, F14, F15 int
		F16, F17, F18, F19, F20 int
		F21, F22, F23, F24, F25 float64
		F26, F27, F28, F29, F30 float64
		F31, F32, F33, F34, F35 bool
		F36, F37, F38, F39, F40 bool
		F41, F42, F43, F44, F45 string
		F46, F47, F48, F49, F50 string
	}

	src := &LargeSource{
		F1: "a", F2: "b", F3: "c", F4: "d", F5: "e",
		F11: 1, F12: 2, F13: 3, F14: 4, F15: 5,
		F21: 1.1, F22: 2.2, F23: 3.3, F24: 4.4, F25: 5.5,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dst := &LargeDest{}
		_ = adapter.Into(dst, src)
	}
}
