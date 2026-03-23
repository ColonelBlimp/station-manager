package adapters

import (
	"reflect"
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test basic pointer-to-struct embedded field
func TestAdapter_PointerEmbeddedStruct(t *testing.T) {
	adapter := New()

	type Address struct {
		Street string
		City   string
	}

	type PersonSrc struct {
		Name string
		*Address
	}

	type PersonDst struct {
		Name   string
		Street string
		City   string
	}

	src := &PersonSrc{
		Name: "Alice",
		Address: &Address{
			Street: "123 Main St",
			City:   "Boston",
		},
	}

	dst := &PersonDst{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Alice", dst.Name)
	assert.Equal(t, "123 Main St", dst.Street)
	assert.Equal(t, "Boston", dst.City)
}

// Test nil pointer-to-struct embedded field
func TestAdapter_NilPointerEmbeddedStruct(t *testing.T) {
	adapter := New()

	type Address struct {
		Street string
		City   string
	}

	type PersonSrc struct {
		Name     string
		*Address // nil pointer
	}

	type PersonDst struct {
		Name   string
		Street string
		City   string
	}

	src := &PersonSrc{
		Name:    "Bob",
		Address: nil, // nil embedded pointer
	}

	dst := &PersonDst{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Bob", dst.Name)
	assert.Equal(t, "", dst.Street) // Zero values since embedded was nil
	assert.Equal(t, "", dst.City)
}

// Test multiple pointer-to-struct embedded fields
func TestAdapter_MultiplePointerEmbedded(t *testing.T) {
	adapter := New()

	type Contact struct {
		Email string
		Phone string
	}

	type Address struct {
		City    string
		Country string
	}

	type PersonSrc struct {
		Name string
		*Contact
		*Address
	}

	type PersonDst struct {
		Name    string
		Email   string
		Phone   string
		City    string
		Country string
	}

	src := &PersonSrc{
		Name: "Charlie",
		Contact: &Contact{
			Email: "charlie@example.com",
			Phone: "555-1234",
		},
		Address: &Address{
			City:    "London",
			Country: "UK",
		},
	}

	dst := &PersonDst{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Charlie", dst.Name)
	assert.Equal(t, "charlie@example.com", dst.Email)
	assert.Equal(t, "555-1234", dst.Phone)
	assert.Equal(t, "London", dst.City)
	assert.Equal(t, "UK", dst.Country)
}

// Test mix of pointer and non-pointer embedded structs
func TestAdapter_MixedEmbeddedTypes(t *testing.T) {
	adapter := New()

	type Contact struct {
		Email string
	}

	type Address struct {
		City string
	}

	type PersonSrc struct {
		Name     string
		Contact  // non-pointer embedded
		*Address // pointer embedded
	}

	type PersonDst struct {
		Name  string
		Email string
		City  string
	}

	src := &PersonSrc{
		Name:    "Diana",
		Contact: Contact{Email: "diana@example.com"},
		Address: &Address{City: "Paris"},
	}

	dst := &PersonDst{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Diana", dst.Name)
	assert.Equal(t, "diana@example.com", dst.Email)
	assert.Equal(t, "Paris", dst.City)
}

// Test pointer embedded to AdditionalData
func TestAdapter_PointerEmbeddedToAdditionalData(t *testing.T) {
	adapter := New()

	type Details struct {
		Age    int
		Height int
	}

	type PersonSrc struct {
		Name string
		*Details
	}

	type PersonDst struct {
		Name           string
		AdditionalData null.JSON
	}

	src := &PersonSrc{
		Name: "Eve",
		Details: &Details{
			Age:    30,
			Height: 170,
		},
	}

	dst := &PersonDst{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Eve", dst.Name)
	assert.True(t, dst.AdditionalData.Valid)

	var additionalFields map[string]interface{}
	err = json.Unmarshal(dst.AdditionalData.JSON, &additionalFields)
	require.NoError(t, err)

	assert.Equal(t, float64(30), additionalFields["Age"])
	assert.Equal(t, float64(170), additionalFields["Height"])
}

// Test AdditionalData to pointer embedded fields
func TestAdapter_AdditionalDataToPointerEmbedded(t *testing.T) {
	adapter := New()

	type Details struct {
		Age    int
		Height int
	}

	type PersonSrc struct {
		Name           string
		AdditionalData null.JSON
	}

	type PersonDst struct {
		Name string
		*Details
	}

	additionalFields := map[string]interface{}{
		"Age":    30,
		"Height": 170,
	}
	jsonData, err := json.Marshal(additionalFields)
	require.NoError(t, err)

	src := &PersonSrc{
		Name:           "Frank",
		AdditionalData: null.JSONFrom(jsonData),
	}

	// Initialize the pointer embedded field
	dst := &PersonDst{
		Details: &Details{},
	}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Frank", dst.Name)
	assert.Equal(t, 30, dst.Details.Age)
	assert.Equal(t, 170, dst.Details.Height)
}

// Test nested pointer embedded structs
func TestAdapter_NestedPointerEmbedded(t *testing.T) {
	adapter := New()

	type Location struct {
		City string
	}

	type Address struct {
		Street string
		*Location
	}

	type PersonSrc struct {
		Name string
		*Address
	}

	type PersonDst struct {
		Name   string
		Street string
		City   string
	}

	src := &PersonSrc{
		Name: "Grace",
		Address: &Address{
			Street: "456 Oak Ave",
			Location: &Location{
				City: "Seattle",
			},
		},
	}

	dst := &PersonDst{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Grace", dst.Name)
	assert.Equal(t, "456 Oak Ave", dst.Street)
	assert.Equal(t, "Seattle", dst.City)
}

// Test pointer embedded with converters
func TestAdapter_PointerEmbeddedWithConverter(t *testing.T) {
	adapter := New()

	type Metadata struct {
		Score float64
	}

	type RecordSrc struct {
		Name string
		*Metadata
	}

	type RecordDst struct {
		Name  string
		Score int
	}

	// Register converter to convert float64 Score to int Score
	adapter.RegisterConverter("Score", func(src interface{}) (interface{}, error) {
		score, ok := src.(float64)
		if !ok {
			return nil, assert.AnError
		}
		return int(score * 10), nil // Scale by 10 and convert to int
	})

	src := &RecordSrc{
		Name: "TestRecord",
		Metadata: &Metadata{
			Score: 8.5,
		},
	}

	dst := &RecordDst{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "TestRecord", dst.Name)
	assert.Equal(t, 85, dst.Score)
}

// Test that field count is accurate with pointer embedded
func TestAdapter_FieldCountWithPointerEmbedded(t *testing.T) {
	adapter := New()

	type Embedded1 struct {
		F1 string
		F2 int
	}

	type Embedded2 struct {
		F3 bool
		F4 float64
	}

	type Parent struct {
		Direct string
		*Embedded1
		*Embedded2
	}

	typ := reflect.TypeOf(Parent{})
	count := adapter.countFields(typ)

	// Should count: Direct (1) + F1, F2 (2) + F3, F4 (2) = 5
	assert.Equal(t, 5, count)

	meta := adapter.getOrBuildMetadata(typ)
	assert.Equal(t, 5, len(meta.fields))
	assert.Equal(t, 5, len(meta.fieldsByName))
}

// Test partially nil pointer embedded structs
func TestAdapter_PartiallyNilPointerEmbedded(t *testing.T) {
	adapter := New()

	type Contact struct {
		Email string
	}

	type Address struct {
		City string
	}

	type PersonSrc struct {
		Name     string
		*Contact // will be non-nil
		*Address // will be nil
	}

	type PersonDst struct {
		Name  string
		Email string
		City  string
	}

	src := &PersonSrc{
		Name:    "Henry",
		Contact: &Contact{Email: "henry@example.com"},
		Address: nil, // nil pointer
	}

	dst := &PersonDst{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Henry", dst.Name)
	assert.Equal(t, "henry@example.com", dst.Email)
	assert.Equal(t, "", dst.City) // Zero value from nil pointer
}

// Test roundtrip with pointer embedded and AdditionalData
func TestAdapter_RoundTripPointerEmbeddedAdditionalData(t *testing.T) {
	adapter := New()

	type Details struct {
		Age    int
		Active bool
	}

	type PersonFull struct {
		Name string
		*Details
		Email string
	}

	type PersonCompact struct {
		Name           string
		AdditionalData null.JSON
	}

	// Step 1: Full -> Compact (marshal to AdditionalData)
	src := &PersonFull{
		Name: "Iris",
		Details: &Details{
			Age:    25,
			Active: true,
		},
		Email: "iris@example.com",
	}

	compact := &PersonCompact{}
	err := adapter.Into(compact, src)
	require.NoError(t, err)

	assert.Equal(t, "Iris", compact.Name)
	assert.True(t, compact.AdditionalData.Valid)

	// Step 2: Compact -> Full (unmarshal from AdditionalData)
	dst := &PersonFull{
		Details: &Details{}, // Initialize pointer
	}
	err = adapter.Into(dst, compact)
	require.NoError(t, err)

	assert.Equal(t, "Iris", dst.Name)
	assert.Equal(t, 25, dst.Details.Age)
	assert.Equal(t, true, dst.Details.Active)
	assert.Equal(t, "iris@example.com", dst.Email)
}
