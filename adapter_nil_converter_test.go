package adapters

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test converter returning nil
func TestAdapter_ConverterReturnsNil(t *testing.T) {
	adapter := New()

	type Source struct {
		Name  string
		Value *int
	}

	type Dest struct {
		Name  string
		Value *int
	}

	// Register converter that returns nil
	adapter.RegisterConverter("Value", func(src interface{}) (interface{}, error) {
		// Intentionally return nil to simulate a converter that nullifies a value
		return nil, nil
	})

	value := 42
	src := &Source{
		Name:  "Test",
		Value: &value,
	}

	dst := &Dest{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Equal(t, "Test", dst.Name)
	assert.Nil(t, dst.Value) // Should be nil due to converter
}

// Test converter returning typed nil interface
func TestAdapter_ConverterReturnsTypedNil(t *testing.T) {
	adapter := New()

	type Source struct {
		Ptr *string
	}

	type Dest struct {
		Ptr *string
	}

	// Register converter that returns typed nil (*string)(nil)
	adapter.RegisterConverter("Ptr", func(src interface{}) (interface{}, error) {
		// Return typed nil
		var result *string
		return result, nil
	})

	str := "test"
	src := &Source{Ptr: &str}
	dst := &Dest{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	assert.Nil(t, dst.Ptr)
}
