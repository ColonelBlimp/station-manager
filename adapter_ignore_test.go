package adapters

import (
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test structs with ignored fields using `adapter:"ignore"` tag
type SourceWithIgnore struct {
	Name     string
	Password string `adapter:"ignore"`
	Email    string
	Token    string `adapter:"ignore"`
}

type DestWithIgnore struct {
	Name     string
	Password string
	Email    string
	Token    string
}

func TestAdapter_IgnoreTag(t *testing.T) {
	adapter := New()

	src := &SourceWithIgnore{
		Name:     "John Doe",
		Password: "secret123",
		Email:    "john@example.com",
		Token:    "abc123xyz",
	}

	dst := &DestWithIgnore{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	// Name and Email should be copied
	assert.Equal(t, "John Doe", dst.Name)
	assert.Equal(t, "john@example.com", dst.Email)

	// Password and Token should NOT be copied (ignored)
	assert.Empty(t, dst.Password)
	assert.Empty(t, dst.Token)
}

// Test structs with ignored fields using `adapter:"-"` tag (alternative syntax)
type SourceWithDashIgnore struct {
	Name     string
	Secret   string `adapter:"-"`
	Email    string
	Internal string `adapter:"-"`
}

type DestWithDashIgnore struct {
	Name     string
	Secret   string
	Email    string
	Internal string
}

func TestAdapter_DashIgnoreTag(t *testing.T) {
	adapter := New()

	src := &SourceWithDashIgnore{
		Name:     "Jane Doe",
		Secret:   "confidential",
		Email:    "jane@example.com",
		Internal: "internal-data",
	}

	dst := &DestWithDashIgnore{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	// Name and Email should be copied
	assert.Equal(t, "Jane Doe", dst.Name)
	assert.Equal(t, "jane@example.com", dst.Email)

	// Secret and Internal should NOT be copied (ignored)
	assert.Empty(t, dst.Secret)
	assert.Empty(t, dst.Internal)
}

// Test that ignored fields are not marshaled to AdditionalData
type SourceWithIgnoreAndExtra struct {
	Name     string
	Password string `adapter:"ignore"`
	Phone    string
	Token    string `adapter:"-"`
	City     string
}

type DestWithIgnoreAndAdditionalData struct {
	Name           string
	AdditionalData null.JSON
}

func TestAdapter_IgnoreNotInAdditionalData(t *testing.T) {
	adapter := New()

	src := &SourceWithIgnoreAndExtra{
		Name:     "Bob Smith",
		Password: "secret456",
		Phone:    "555-1234",
		Token:    "xyz789",
		City:     "Boston",
	}

	dst := &DestWithIgnoreAndAdditionalData{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	// Name should be copied directly
	assert.Equal(t, "Bob Smith", dst.Name)

	// AdditionalData should contain Phone and City, but NOT Password or Token
	require.True(t, dst.AdditionalData.Valid)

	var additionalFields map[string]interface{}
	err = dst.AdditionalData.Unmarshal(&additionalFields)
	require.NoError(t, err)

	// Phone and City should be in AdditionalData
	assert.Equal(t, "555-1234", additionalFields["Phone"])
	assert.Equal(t, "Boston", additionalFields["City"])

	// Password and Token should NOT be in AdditionalData
	_, hasPassword := additionalFields["Password"]
	_, hasToken := additionalFields["Token"]
	assert.False(t, hasPassword, "Password should not be in AdditionalData")
	assert.False(t, hasToken, "Token should not be in AdditionalData")
}

// Test ignored fields in destination struct
type SourceForDestIgnore struct {
	Name     string
	Password string
	Email    string
}

type DestWithIgnoredFields struct {
	Name     string
	Password string `adapter:"ignore"`
	Email    string
}

func TestAdapter_IgnoreDestinationField(t *testing.T) {
	adapter := New()

	src := &SourceForDestIgnore{
		Name:     "Alice Brown",
		Password: "should-not-copy",
		Email:    "alice@example.com",
	}

	dst := &DestWithIgnoredFields{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	// Name and Email should be copied
	assert.Equal(t, "Alice Brown", dst.Name)
	assert.Equal(t, "alice@example.com", dst.Email)

	// Password should NOT be copied because destination field is ignored
	assert.Empty(t, dst.Password)
}

// Test ignored fields with converters
type SourceWithIgnoreAndConverter struct {
	Name        string
	Temperature float64 `adapter:"ignore"`
	Email       string
}

type DestWithIgnoreAndConverter struct {
	Name        string
	Temperature int
	Email       string
}

func TestAdapter_IgnoreWithConverter(t *testing.T) {
	adapter := New()

	// Register converter for Temperature field (should not be used due to ignore)
	adapter.RegisterConverter("Temperature", func(src interface{}) (interface{}, error) {
		temp := src.(float64)
		return int(temp), nil
	})

	src := &SourceWithIgnoreAndConverter{
		Name:        "Charlie Davis",
		Temperature: 98.6,
		Email:       "charlie@example.com",
	}

	dst := &DestWithIgnoreAndConverter{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	// Name and Email should be copied
	assert.Equal(t, "Charlie Davis", dst.Name)
	assert.Equal(t, "charlie@example.com", dst.Email)

	// Temperature should NOT be copied (ignored), even though a converter exists
	assert.Equal(t, 0, dst.Temperature)
}

// Test mixed: some ignored, some not
type SourceMixed struct {
	PublicField1  string
	PrivateField1 string `adapter:"ignore"`
	PublicField2  int
	PrivateField2 int `adapter:"-"`
	PublicField3  bool
}

type DestMixed struct {
	PublicField1  string
	PrivateField1 string
	PublicField2  int
	PrivateField2 int
	PublicField3  bool
}

func TestAdapter_MixedIgnoreFields(t *testing.T) {
	adapter := New()

	src := &SourceMixed{
		PublicField1:  "public1",
		PrivateField1: "private1",
		PublicField2:  42,
		PrivateField2: 99,
		PublicField3:  true,
	}

	dst := &DestMixed{}

	err := adapter.Into(dst, src)
	require.NoError(t, err)

	// Public fields should be copied
	assert.Equal(t, "public1", dst.PublicField1)
	assert.Equal(t, 42, dst.PublicField2)
	assert.True(t, dst.PublicField3)

	// Private fields should NOT be copied
	assert.Empty(t, dst.PrivateField1)
	assert.Equal(t, 0, dst.PrivateField2)
}

// Test that ignore tag doesn't affect AdditionalData unmarshaling
type SourceWithAdditionalDataForIgnore struct {
	Name           string
	AdditionalData null.JSON
}

type DestWithIgnoreFromAdditionalData struct {
	Name     string
	Password string `adapter:"ignore"`
	Email    string
	Phone    string
}

func TestAdapter_IgnoreDoesNotAffectAdditionalDataUnmarshaling(t *testing.T) {
	adapter := New()

	// Create source with AdditionalData containing Email, Phone, and Password
	additionalData := map[string]interface{}{
		"Email":    "test@example.com",
		"Phone":    "555-9999",
		"Password": "from-additional-data",
	}
	jsonBytes, err := json.Marshal(additionalData)
	require.NoError(t, err)

	src := &SourceWithAdditionalDataForIgnore{
		Name:           "Test User",
		AdditionalData: null.JSONFrom(jsonBytes),
	}

	dst := &DestWithIgnoreFromAdditionalData{}

	err = adapter.Into(dst, src)
	require.NoError(t, err)

	// Name should be copied
	assert.Equal(t, "Test User", dst.Name)

	// Email and Phone should be unmarshaled from AdditionalData
	assert.Equal(t, "test@example.com", dst.Email)
	assert.Equal(t, "555-9999", dst.Phone)

	// Password should NOT be unmarshaled because it's ignored in destination
	assert.Empty(t, dst.Password)
}
