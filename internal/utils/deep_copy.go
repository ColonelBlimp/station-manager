package utils

import (
	"fmt"
	"github.com/goccy/go-json"
)

// DeepCopy performs a deep copy of any object using JSON serialization and deserialization.
// The input `in` is the object to copy, and the output `out` should be a pointer to the desired type.
func DeepCopy(in interface{}, out interface{}) error {
	// Marshal the input object to JSON
	serialized, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("failed to serialize input: %w", err)
	}

	// Unmarshal the JSON into the output object
	if err = json.Unmarshal(serialized, out); err != nil {
		return fmt.Errorf("failed to deserialize into output: %w", err)
	}

	return nil
}
