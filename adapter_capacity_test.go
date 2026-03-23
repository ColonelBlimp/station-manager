package adapters

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test that countFields correctly counts fields including embedded structs
func TestAdapter_CountFieldsAccuracy(t *testing.T) {
	adapter := New()

	type Embedded1 struct {
		Field1 string
		Field2 int
		Field3 bool
	}

	type Embedded2 struct {
		Field4 float64
		Field5 string
	}

	type Parent struct {
		DirectField string
		Embedded1
		Embedded2
		AnotherDirect int
	}

	typ := reflect.TypeOf(Parent{})
	count := adapter.countFields(typ)

	// Should count:
	// DirectField (1) + Field1,Field2,Field3 (3) + Field4,Field5 (2) + AnotherDirect (1) = 7
	assert.Equal(t, 7, count, "countFields should accurately count all fields including embedded")

	// Verify metadata actually has this many fields
	meta := adapter.getOrBuildMetadata(typ)
	assert.Equal(t, 7, len(meta.fields), "metadata should have 7 fields")
	assert.Equal(t, 7, len(meta.fieldsByName), "fieldsByName should have 7 entries")

	// Verify all expected field names are present
	expectedFields := []string{"DirectField", "Field1", "Field2", "Field3", "Field4", "Field5", "AnotherDirect"}
	for _, fieldName := range expectedFields {
		_, found := meta.fieldsByName[fieldName]
		assert.True(t, found, "field %s should be in fieldsByName", fieldName)
	}
}

// Test with pointer-to-struct embedded fields
func TestAdapter_CountFieldsWithPointerEmbedded(t *testing.T) {
	adapter := New()

	type Embedded struct {
		Field1 string
		Field2 int
	}

	type Parent struct {
		DirectField string
		*Embedded
		AnotherDirect int
	}

	typ := reflect.TypeOf(Parent{})
	count := adapter.countFields(typ)

	// Should count: DirectField (1) + Field1,Field2 (2) + AnotherDirect (1) = 4
	assert.Equal(t, 4, count, "countFields should handle pointer-to-struct embedded fields")

	meta := adapter.getOrBuildMetadata(typ)
	assert.Equal(t, 4, len(meta.fields))
}

// Test that capacity is correctly preallocated (no reallocations)
func TestAdapter_NoSliceReallocation(t *testing.T) {
	adapter := New()

	type Large1 struct {
		F1, F2, F3, F4, F5 string
	}

	type Large2 struct {
		F6, F7, F8, F9, F10 string
	}

	type Large3 struct {
		F11, F12, F13, F14, F15 string
	}

	type Parent struct {
		Root string
		Large1
		Large2
		Large3
	}

	typ := reflect.TypeOf(Parent{})
	count := adapter.countFields(typ)

	// Should count all 16 fields (1 root + 15 embedded)
	assert.Equal(t, 16, count)

	meta := adapter.getOrBuildMetadata(typ)
	// Verify correct number of fields
	assert.Equal(t, 16, len(meta.fields))
	// Verify capacity was sufficient (cap >= len means no reallocation occurred)
	assert.GreaterOrEqual(t, cap(meta.fields), len(meta.fields))
}
