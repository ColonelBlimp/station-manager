package utils

import (
	"fmt"
	"reflect"
)

// SetStructStringField sets the value of a string field in a struct by its field name dynamically using reflection.
// Requires a pointer to a struct; returns an error if v is not a pointer to a struct, the field does not exist, or cannot be set.
func SetStructStringField(v interface{}, fieldName string, value string) error {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return fmt.Errorf("expected a pointer to a struct, got: %s", val.Kind())
	}
	val = val.Elem()
	if val.Kind() != reflect.Struct {
		return fmt.Errorf("expected a pointer to a struct, got pointer to: %s", val.Kind())
	}

	field := val.FieldByName(fieldName)
	if !field.IsValid() {
		return fmt.Errorf("no such field: %s in struct", fieldName)
	}
	if !field.CanSet() {
		return fmt.Errorf("cannot set field: %s", fieldName)
	}
	if field.Kind() != reflect.String {
		return fmt.Errorf("field: %s is not a string", fieldName)
	}

	field.SetString(value)
	return nil
}
