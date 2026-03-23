package adapters

import (
	"fmt"
	"github.com/goccy/go-json"
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test structs for basic field copying
type SourceBasic struct {
	Name  string
	Age   int
	Email string
}

type DestBasic struct {
	Name  string
	Age   int
	Email string
}

func TestAdapter_BasicFieldCopy(t *testing.T) {
	adapter := New()

	src := &SourceBasic{
		Name:  "John Doe",
		Age:   30,
		Email: "john@example.com",
	}

	dst := &DestBasic{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, src.Name, dst.Name)
	assert.Equal(t, src.Age, dst.Age)
	assert.Equal(t, src.Email, dst.Email)
}

// Test structs with converter function
type SourceWithConverter struct {
	Temperature float64
}

type DestWithConverter struct {
	Temperature int // Celsius as integer
}

func TestAdapter_WithConverter(t *testing.T) {
	adapter := New()

	// Register converter for Temperature field
	adapter.RegisterConverter("Temperature", func(src interface{}) (interface{}, error) {
		temp, ok := src.(float64)
		if !ok {
			return nil, fmt.Errorf("expected float64, got %T", src)
		}
		return int(temp), nil
	})

	src := &SourceWithConverter{Temperature: 25.7}
	dst := &DestWithConverter{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, 25, dst.Temperature)
}

// Test structs with AdditionalData marshaling
type SourceWithExtra struct {
	Name   string
	Age    int
	Email  string
	Phone  string
	City   string
	Active bool
}

type DestWithAdditionalData struct {
	Name           string
	Age            int
	AdditionalData null.JSON
}

func TestAdapter_MarshalToAdditionalData(t *testing.T) {
	adapter := New()

	src := &SourceWithExtra{
		Name:   "Jane Doe",
		Age:    25,
		Email:  "jane@example.com",
		Phone:  "123-456-7890",
		City:   "New York",
		Active: true,
	}

	dst := &DestWithAdditionalData{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Jane Doe", dst.Name)
	assert.Equal(t, 25, dst.Age)

	hasData := dst.AdditionalData.Valid
	jsonData := dst.AdditionalData.JSON
	assert.True(t, hasData)

	// Verify AdditionalData contains the remaining fields
	var additionalFields map[string]interface{}
	err = json.Unmarshal(jsonData, &additionalFields)
	require.NoError(t, err)

	assert.Equal(t, "jane@example.com", additionalFields["Email"])
	assert.Equal(t, "123-456-7890", additionalFields["Phone"])
	assert.Equal(t, "New York", additionalFields["City"])
	assert.Equal(t, true, additionalFields["Active"])
}

// Test structs with AdditionalData unmarshaling
type SourceWithAdditionalData struct {
	Name           string
	Age            int
	AdditionalData null.JSON
}

type DestExpanded struct {
	Name   string
	Age    int
	Email  string
	Phone  string
	City   string
	Active bool
}

func TestAdapter_UnmarshalFromAdditionalData(t *testing.T) {
	adapter := New()

	additionalFields := map[string]interface{}{
		"Email":  "bob@example.com",
		"Phone":  "555-1234",
		"City":   "Boston",
		"Active": true,
	}
	jsonData, err := json.Marshal(additionalFields)
	require.NoError(t, err)

	src := &SourceWithAdditionalData{
		Name:           "Bob Smith",
		Age:            40,
		AdditionalData: null.JSONFrom(jsonData),
	}

	dst := &DestExpanded{}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Bob Smith", dst.Name)
	assert.Equal(t, 40, dst.Age)
	assert.Equal(t, "bob@example.com", dst.Email)
	assert.Equal(t, "555-1234", dst.Phone)
	assert.Equal(t, "Boston", dst.City)
	assert.Equal(t, true, dst.Active)
}

// Test that direct fields take precedence over AdditionalData
func TestAdapter_DirectFieldsPrecedence(t *testing.T) {
	adapter := New()

	additionalFields := map[string]interface{}{
		"Name": "Should Not Appear",
		"Age":  999,
	}
	jsonData, err := json.Marshal(additionalFields)
	require.NoError(t, err)

	src := &SourceWithAdditionalData{
		Name:           "Correct Name",
		Age:            35,
		AdditionalData: null.JSONFrom(jsonData),
	}

	dst := &DestBasic{}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	// Direct fields should take precedence
	assert.Equal(t, "Correct Name", dst.Name)
	assert.Equal(t, 35, dst.Age)
}

// Test converter with AdditionalData
type SourceWithBothConverterAndAdditional struct {
	Temperature    float64
	AdditionalData null.JSON
}

type DestWithBothConverterAndAdditional struct {
	Temperature int
	Humidity    int
}

func TestAdapter_ConverterWithAdditionalData(t *testing.T) {
	adapter := New()

	// Register converter for Temperature
	adapter.RegisterConverter("Temperature", func(src interface{}) (interface{}, error) {
		temp, ok := src.(float64)
		if !ok {
			return nil, fmt.Errorf("expected float64, got %T", src)
		}
		return int(temp), nil
	})

	// Register converter for Humidity (from AdditionalData)
	adapter.RegisterConverter("Humidity", func(src interface{}) (interface{}, error) {
		// JSON unmarshals numbers as float64
		humidity, ok := src.(float64)
		if !ok {
			return nil, fmt.Errorf("expected float64, got %T", src)
		}
		return int(humidity), nil
	})

	additionalFields := map[string]interface{}{
		"Humidity": 65.0,
	}
	jsonData, err := json.Marshal(additionalFields)
	require.NoError(t, err)

	src := &SourceWithBothConverterAndAdditional{
		Temperature:    22.8,
		AdditionalData: null.JSONFrom(jsonData),
	}

	dst := &DestWithBothConverterAndAdditional{}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, 22, dst.Temperature)
	assert.Equal(t, 65, dst.Humidity)
}

// Test error cases
func TestAdapter_ErrorCases(t *testing.T) {
	adapter := New()

	t.Run("nil source", func(t *testing.T) {
		dst := &DestBasic{}
		err := adapter.Into(dst, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must not be nil")
	})

	t.Run("nil destination", func(t *testing.T) {
		src := &SourceBasic{}
		err := adapter.Into(nil, src)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must not be nil")
	})

	t.Run("non-pointer source", func(t *testing.T) {
		src := SourceBasic{}
		dst := &DestBasic{}
		err := adapter.Into(dst, src)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be pointers")
	})

	t.Run("non-pointer destination", func(t *testing.T) {
		src := &SourceBasic{}
		dst := DestBasic{}
		err := adapter.Into(dst, src)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be pointers")
	})

	t.Run("non-struct source", func(t *testing.T) {
		src := "not a struct"
		dst := &DestBasic{}
		err := adapter.Into(dst, &src)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must point to structs")
	})
}

// Test converter error handling
func TestAdapter_ConverterError(t *testing.T) {
	adapter := New()

	adapter.RegisterConverter("Temperature", func(src interface{}) (interface{}, error) {
		return nil, fmt.Errorf("conversion failed")
	})

	src := &SourceWithConverter{Temperature: 25.7}
	dst := &DestWithConverter{}

	err := adapter.Into(dst, src)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conversion failed")
}

// Test partial field matches
type SourcePartial struct {
	Name  string
	Age   int
	Email string
}

type DestPartial struct {
	Name string
	Age  int
}

func TestAdapter_PartialFieldMatch(t *testing.T) {
	adapter := New()

	src := &SourcePartial{
		Name:  "Alice",
		Age:   28,
		Email: "alice@example.com",
	}

	dst := &DestPartial{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Alice", dst.Name)
	assert.Equal(t, 28, dst.Age)
}

// Test type conversion (int to int64, etc.)
type SourceTyped struct {
	Count  int
	Amount float32
}

type DestTyped struct {
	Count  int64
	Amount float64
}

func TestAdapter_TypeConversion(t *testing.T) {
	adapter := New()

	src := &SourceTyped{
		Count:  42,
		Amount: 123.45,
	}

	dst := &DestTyped{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, int64(42), dst.Count)
	assert.InDelta(t, 123.45, dst.Amount, 0.01)
}

// Test empty AdditionalData
func TestAdapter_EmptyAdditionalData(t *testing.T) {
	adapter := New()

	src := &SourceBasic{
		Name:  "Test",
		Age:   30,
		Email: "test@example.com",
	}

	dst := &DestWithAdditionalData{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Test", dst.Name)
	assert.Equal(t, 30, dst.Age)

	hasData := dst.AdditionalData.Valid
	jsonData := dst.AdditionalData.JSON
	assert.True(t, hasData)

	var additionalFields map[string]interface{}
	err = json.Unmarshal(jsonData, &additionalFields)
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", additionalFields["Email"])
}

// Test with invalid AdditionalData in source
func TestAdapter_InvalidAdditionalData(t *testing.T) {
	adapter := New()

	src := &SourceWithAdditionalData{
		Name:           "Test",
		Age:            30,
		AdditionalData: null.JSONFrom([]byte("invalid json")),
	}

	dst := &DestExpanded{}

	err := adapter.Into(dst, src)
	// Should handle invalid JSON gracefully
	assert.Error(t, err)
}

// Test non-null.JSON AdditionalData (should be ignored)
type SourceWithNonJSONAdditional struct {
	Name           string
	Age            int
	AdditionalData string // Not null.JSON, should be ignored
}

func TestAdapter_NonJSONAdditionalData(t *testing.T) {
	adapter := New()

	src := &SourceWithNonJSONAdditional{
		Name:           "Test",
		Age:            30,
		AdditionalData: "ignored",
	}

	dst := &DestBasic{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Test", dst.Name)
	assert.Equal(t, 30, dst.Age)
}

// Test concurrent access
func TestAdapter_ConcurrentAccess(t *testing.T) {
	adapter := New()

	// Register some converters
	for i := 0; i < 10; i++ {
		fieldName := fmt.Sprintf("Field%d", i)
		adapter.RegisterConverter(fieldName, func(src interface{}) (interface{}, error) {
			return src, nil
		})
	}

	// Run multiple adaptations concurrently
	const goroutines = 100
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			src := &SourceBasic{Name: "Test", Age: 30, Email: "test@example.com"}
			dst := &DestBasic{}
			_ = adapter.Into(dst, src)
			done <- true
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// Test null AdditionalData
func TestAdapter_NullAdditionalData(t *testing.T) {
	adapter := New()

	src := &SourceWithAdditionalData{
		Name:           "Test",
		Age:            30,
		AdditionalData: null.JSON{},
	}

	dst := &DestExpanded{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Test", dst.Name)
	assert.Equal(t, 30, dst.Age)
	// Other fields should have zero values
	assert.Equal(t, "", dst.Email)
	assert.Equal(t, "", dst.Phone)
}

// Test round-trip: marshal to AdditionalData and unmarshal back
func TestAdapter_RoundTrip(t *testing.T) {
	adapter := New()

	// Step 1: Adapt from expanded to compact (with AdditionalData)
	src1 := &SourceWithExtra{
		Name:   "Round Trip",
		Age:    45,
		Email:  "round@example.com",
		Phone:  "999-8888",
		City:   "Chicago",
		Active: true,
	}

	intermediate := &DestWithAdditionalData{}

	err := adapter.Into(intermediate, src1)
	require.NoError(t, err)

	// Step 2: Adapt from compact back to expanded
	dst := &DestExpanded{}

	err = adapter.Into(dst, intermediate)
	require.NoError(t, err)

	// Verify all fields are preserved
	assert.Equal(t, "Round Trip", dst.Name)
	assert.Equal(t, 45, dst.Age)
	assert.Equal(t, "round@example.com", dst.Email)
	assert.Equal(t, "999-8888", dst.Phone)
	assert.Equal(t, "Chicago", dst.City)
	assert.Equal(t, true, dst.Active)
}

// Test when all source fields are mapped to destination (no remaining fields for AdditionalData)
func TestAdapter_NoRemainingFields(t *testing.T) {
	adapter := New()

	src := &SourceBasic{
		Name:  "Complete",
		Age:   50,
		Email: "complete@example.com",
	}

	// Destination has same fields plus AdditionalData
	type DestWithAdditionalDataComplete struct {
		Name           string
		Age            int
		Email          string
		AdditionalData null.JSON
	}

	dst := &DestWithAdditionalDataComplete{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Complete", dst.Name)
	assert.Equal(t, 50, dst.Age)
	assert.Equal(t, "complete@example.com", dst.Email)

	// AdditionalData should be null since there are no remaining fields
	hasData := dst.AdditionalData.Valid
	assert.False(t, hasData)
}

// Test converter that returns wrong type
func TestAdapter_ConverterWrongType(t *testing.T) {
	adapter := New()

	adapter.RegisterConverter("Age", func(src interface{}) (interface{}, error) {
		// Return wrong type (string instead of int)
		return "not an int", nil
	})

	src := &SourceBasic{
		Name:  "Test",
		Age:   30,
		Email: "test@example.com",
	}

	dst := &DestBasic{}

	err := adapter.Into(dst, src)
	// Should fail because converter returns wrong type
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "converter returned type")
}

// Test field that can't be converted
type SourceIncompatible struct {
	Name string
	Data []byte
}

type DestIncompatible struct {
	Name string
	Data map[string]string // Incompatible with []byte
}

func TestAdapter_IncompatibleTypes(t *testing.T) {
	adapter := New()

	src := &SourceIncompatible{
		Name: "Test",
		Data: []byte("data"),
	}

	dst := &DestIncompatible{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	// Name should be copied
	assert.Equal(t, "Test", dst.Name)
	// Data should be skipped (incompatible types)
	assert.Nil(t, dst.Data)
}

// Test converter error in AdditionalData unmarshaling
func TestAdapter_ConverterErrorInAdditionalData(t *testing.T) {
	adapter := New()

	// Register converter that will fail
	adapter.RegisterConverter("Email", func(src interface{}) (interface{}, error) {
		return nil, fmt.Errorf("converter error")
	})

	additionalFields := map[string]interface{}{
		"Email": "test@example.com",
	}
	jsonData, err := json.Marshal(additionalFields)
	require.NoError(t, err)

	src := &SourceWithAdditionalData{
		Name:           "Test",
		Age:            30,
		AdditionalData: null.JSONFrom(jsonData),
	}

	dst := &DestExpanded{}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	// Email should not be set due to converter error
	assert.Equal(t, "", dst.Email)
}

// Test converter in AdditionalData that returns incompatible type
func TestAdapter_ConverterIncompatibleTypeInAdditionalData(t *testing.T) {
	adapter := New()

	// Register converter for Phone that returns incompatible type
	adapter.RegisterConverter("Phone", func(src interface{}) (interface{}, error) {
		return 12345, nil // Returns int instead of string
	})

	additionalFields := map[string]interface{}{
		"Phone": "555-1234",
		"Email": "test@example.com",
	}
	jsonData, err := json.Marshal(additionalFields)
	require.NoError(t, err)

	src := &SourceWithAdditionalData{
		Name:           "Test",
		Age:            30,
		AdditionalData: null.JSONFrom(jsonData),
	}

	dst := &DestExpanded{}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	// Name and Age from direct fields should be set
	assert.Equal(t, "Test", dst.Name)
	assert.Equal(t, 30, dst.Age)
	// Phone should not be set due to converter returning incompatible type
	assert.Equal(t, "", dst.Phone)
	// Email should be set normally
	assert.Equal(t, "test@example.com", dst.Email)
}

// Test structs with embedded fields
type EmbeddedDetails struct {
	Phone   string
	City    string
	Country string
}

type SourceWithEmbedded struct {
	Name string
	Age  int
	EmbeddedDetails
}

type DestWithEmbeddedAdditionalData struct {
	Name           string
	Age            int
	AdditionalData null.JSON
}

func TestAdapter_EmbeddedFieldsToAdditionalData(t *testing.T) {
	adapter := New()

	src := &SourceWithEmbedded{
		Name: "John Smith",
		Age:  35,
		EmbeddedDetails: EmbeddedDetails{
			Phone:   "555-0100",
			City:    "London",
			Country: "UK",
		},
	}

	dst := &DestWithEmbeddedAdditionalData{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "John Smith", dst.Name)
	assert.Equal(t, 35, dst.Age)

	// Verify embedded struct fields are in AdditionalData
	assert.True(t, dst.AdditionalData.Valid)

	var additionalFields map[string]interface{}
	err = json.Unmarshal(dst.AdditionalData.JSON, &additionalFields)
	require.NoError(t, err)

	assert.Equal(t, "555-0100", additionalFields["Phone"])
	assert.Equal(t, "London", additionalFields["City"])
	assert.Equal(t, "UK", additionalFields["Country"])
}

// Test multiple embedded structs
type EmbeddedContact struct {
	Email string
	Phone string
}

type EmbeddedAddress struct {
	City    string
	Country string
}

type SourceWithMultipleEmbedded struct {
	Name string
	Age  int
	EmbeddedContact
	EmbeddedAddress
}

func TestAdapter_MultipleEmbeddedFieldsToAdditionalData(t *testing.T) {
	adapter := New()

	src := &SourceWithMultipleEmbedded{
		Name: "Jane Doe",
		Age:  28,
		EmbeddedContact: EmbeddedContact{
			Email: "jane@example.com",
			Phone: "555-0200",
		},
		EmbeddedAddress: EmbeddedAddress{
			City:    "Paris",
			Country: "France",
		},
	}

	dst := &DestWithEmbeddedAdditionalData{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Jane Doe", dst.Name)
	assert.Equal(t, 28, dst.Age)

	// Verify all embedded struct fields are in AdditionalData
	assert.True(t, dst.AdditionalData.Valid)

	var additionalFields map[string]interface{}
	err = json.Unmarshal(dst.AdditionalData.JSON, &additionalFields)
	require.NoError(t, err)

	assert.Equal(t, "jane@example.com", additionalFields["Email"])
	assert.Equal(t, "555-0200", additionalFields["Phone"])
	assert.Equal(t, "Paris", additionalFields["City"])
	assert.Equal(t, "France", additionalFields["Country"])
}
