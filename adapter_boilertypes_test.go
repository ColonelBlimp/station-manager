package adapters

import (
	"testing"

	boilertypes "github.com/aarondl/sqlboiler/v4/types"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test structs with boilertypes.JSON (SQLBoiler JSON type)
type SourceWithBoilerJSON struct {
	Name           string
	Age            int
	AdditionalData boilertypes.JSON
}

type DestFromBoilerJSON struct {
	Name   string
	Age    int
	Email  string
	Phone  string
	City   string
	Active bool
}

func TestAdapter_UnmarshalFromBoilerJSON(t *testing.T) {
	adapter := New()

	additionalFields := map[string]interface{}{
		"Email":  "test@example.com",
		"Phone":  "555-1234",
		"City":   "Boston",
		"Active": true,
	}
	jsonData, err := json.Marshal(additionalFields)
	require.NoError(t, err)

	src := &SourceWithBoilerJSON{
		Name:           "Test User",
		Age:            30,
		AdditionalData: boilertypes.JSON(jsonData),
	}

	dst := &DestFromBoilerJSON{}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Test User", dst.Name)
	assert.Equal(t, 30, dst.Age)
	assert.Equal(t, "test@example.com", dst.Email)
	assert.Equal(t, "555-1234", dst.Phone)
	assert.Equal(t, "Boston", dst.City)
	assert.Equal(t, true, dst.Active)
}

func TestAdapter_EmptyBoilerJSON(t *testing.T) {
	adapter := New()

	src := &SourceWithBoilerJSON{
		Name:           "Test User",
		Age:            30,
		AdditionalData: boilertypes.JSON{},
	}

	dst := &DestFromBoilerJSON{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Test User", dst.Name)
	assert.Equal(t, 30, dst.Age)
	// Other fields should remain zero values
	assert.Equal(t, "", dst.Email)
	assert.Equal(t, "", dst.Phone)
	assert.Equal(t, "", dst.City)
	assert.Equal(t, false, dst.Active)
}

func TestAdapter_NilBoilerJSON(t *testing.T) {
	adapter := New()

	src := &SourceWithBoilerJSON{
		Name:           "Test User",
		Age:            30,
		AdditionalData: nil,
	}

	dst := &DestFromBoilerJSON{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Test User", dst.Name)
	assert.Equal(t, 30, dst.Age)
	// Other fields should remain zero values
	assert.Equal(t, "", dst.Email)
}

func TestAdapter_InvalidBoilerJSON(t *testing.T) {
	adapter := New()

	src := &SourceWithBoilerJSON{
		Name:           "Test User",
		Age:            30,
		AdditionalData: boilertypes.JSON([]byte("invalid json")),
	}

	dst := &DestFromBoilerJSON{}

	err := adapter.Into(dst, src)
	// Should handle invalid JSON gracefully
	assert.Error(t, err)
}

// Test marshaling to boilertypes.JSON
type SourceForBoilerJSON struct {
	Name   string
	Age    int
	Email  string
	Phone  string
	City   string
	Active bool
}

type DestWithBoilerJSON struct {
	Name           string
	Age            int
	AdditionalData boilertypes.JSON
}

func TestAdapter_MarshalToBoilerJSON(t *testing.T) {
	adapter := New()

	src := &SourceForBoilerJSON{
		Name:   "Test User",
		Age:    30,
		Email:  "test@example.com",
		Phone:  "555-1234",
		City:   "Boston",
		Active: true,
	}

	dst := &DestWithBoilerJSON{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Test User", dst.Name)
	assert.Equal(t, 30, dst.Age)

	// Verify AdditionalData contains the remaining fields
	assert.NotNil(t, dst.AdditionalData)
	assert.NotEmpty(t, dst.AdditionalData)

	var additionalFields map[string]interface{}
	err = json.Unmarshal(dst.AdditionalData, &additionalFields)
	require.NoError(t, err)

	assert.Equal(t, "test@example.com", additionalFields["Email"])
	assert.Equal(t, "555-1234", additionalFields["Phone"])
	assert.Equal(t, "Boston", additionalFields["City"])
	assert.Equal(t, true, additionalFields["Active"])
}

// Test round-trip with boilertypes.JSON
func TestAdapter_BoilerJSONRoundTrip(t *testing.T) {
	adapter := New()

	// Step 1: Marshal to BoilerJSON
	src := &SourceForBoilerJSON{
		Name:   "Round Trip",
		Age:    45,
		Email:  "round@example.com",
		Phone:  "999-8888",
		City:   "Chicago",
		Active: true,
	}

	intermediate := &DestWithBoilerJSON{}

	err := adapter.Into(intermediate, src)
	require.NoError(t, err)

	// Step 2: Unmarshal from BoilerJSON
	dst := &DestFromBoilerJSON{}

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

// Test converter with boilertypes.JSON
func TestAdapter_ConverterWithBoilerJSON(t *testing.T) {
	adapter := New()

	// Register converter for Phone field
	adapter.RegisterConverter("Phone", func(src interface{}) (interface{}, error) {
		// Convert from string to formatted phone
		phone, ok := src.(string)
		if !ok {
			return "", nil
		}
		return "(" + phone[:3] + ") " + phone[3:6] + "-" + phone[6:], nil
	})

	additionalFields := map[string]interface{}{
		"Email": "test@example.com",
		"Phone": "5551234567",
	}
	jsonData, err := json.Marshal(additionalFields)
	require.NoError(t, err)

	src := &SourceWithBoilerJSON{
		Name:           "Test User",
		Age:            30,
		AdditionalData: boilertypes.JSON(jsonData),
	}

	dst := &DestFromBoilerJSON{}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Test User", dst.Name)
	assert.Equal(t, 30, dst.Age)
	assert.Equal(t, "test@example.com", dst.Email)
	assert.Equal(t, "(555) 123-4567", dst.Phone)
}

// Test precedence: direct fields should override boilertypes.JSON
func TestAdapter_BoilerJSONPrecedence(t *testing.T) {
	adapter := New()

	additionalFields := map[string]interface{}{
		"Name": "Should Not Appear",
		"Age":  999,
	}
	jsonData, err := json.Marshal(additionalFields)
	require.NoError(t, err)

	src := &SourceWithBoilerJSON{
		Name:           "Correct Name",
		Age:            35,
		AdditionalData: boilertypes.JSON(jsonData),
	}

	type DestBasicFromBoiler struct {
		Name  string
		Age   int
		Email string
	}

	dst := &DestBasicFromBoiler{}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	// Direct fields should take precedence
	assert.Equal(t, "Correct Name", dst.Name)
	assert.Equal(t, 35, dst.Age)
}
