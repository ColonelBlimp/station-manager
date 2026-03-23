package logging

import (
	"fmt"
	"reflect"

	"github.com/rs/zerolog"
)

// Dump logs the contents of the provided value at Debug level.
// It handles various types including structs, maps, slices, and basic types.
// For structs, it logs all exported fields. For complex types like maps and slices,
// it logs their elements. For basic types, it logs their values. The traversal
// depth is limited to avoid stack overflows and it tracks visited addresses to
// avoid cycles.
func (s *Service) Dump(v interface{}) {
	if s == nil || !s.isInitialized.Load() {
		return
	}

	// Increment active operations counter
	s.activeOps.Add(1)
	s.wg.Add(1)
	defer func() {
		s.activeOps.Add(-1)
		s.wg.Done()
	}()

	// Acquire read lock to prevent Close() from running
	s.mu.RLock()

	// Double-check after acquiring lock
	if !s.isInitialized.Load() {
		s.mu.RUnlock()
		return
	}

	logger := s.logger.Load()
	if logger == nil {
		s.mu.RUnlock()
		return
	}

	if v == nil {
		logger.Debug().Msg("Dump: <nil>")
		s.mu.RUnlock()
		return
	}

	// Hold the read lock for the entire operation to prevent Close() from
	// deallocating resources while dumpValue is executing
	defer s.mu.RUnlock()

	// Use a map to track visited pointers to prevent infinite recursion
	visited := make(map[uintptr]bool)
	s.dumpValue(logger, v, "", visited, 0)
}

// Maximum recursion depth to prevent stack overflow
const maxDumpDepth = 10

// dumpValue is a recursive helper function for Dump. It unwraps interfaces and pointers safely
// (with cycle detection) and logs the structure using Debug-level entries.
func (s *Service) dumpValue(logger *zerolog.Logger, v interface{}, prefix string, visited map[uintptr]bool, depth int) {
	if depth > maxDumpDepth {
		logger.Debug().Msgf("%s: <max depth reached>", prefix)
		return
	}

	if v == nil {
		logger.Debug().Msgf("%s: <nil>", prefix)
		return
	}

	val := reflect.ValueOf(v)

	// Safely unwrap interfaces and handle pointers, with cycle detection.
	// Avoid calling Pointer() on unsupported kinds.
	for {
		switch val.Kind() {
		case reflect.Interface:
			if val.IsNil() {
				logger.Debug().Msgf("%s: <nil>", prefix)
				return
			}
			val = val.Elem()
			// continue unwrapping
			continue
		case reflect.Ptr:
			if val.IsNil() {
				logger.Debug().Msgf("%s: <nil>", prefix)
				return
			}
			ptr := val.Pointer()
			if visited[ptr] {
				logger.Debug().Msgf("%s: <circular reference>", prefix)
				return
			}
			visited[ptr] = true
			val = val.Elem()
		// pointer unwrapped; continue handling concrete kind
		default:
			// No-op
		}
		break
	}

	typ := val.Type()

	// For non-pointer addressable values (like structs that are reachable multiple
	// times by reference), record their address to help detect cycles.
	if val.CanAddr() {
		addrPtr := val.Addr().Pointer()
		if visited[addrPtr] {
			logger.Debug().Msgf("%s: <circular reference>", prefix)
			return
		}
		// mark addressable value as visited so repeated references won't recurse endlessly
		visited[addrPtr] = true
		// Note: keep this entry; it's fine for the scope of this dump call
	}

	switch val.Kind() {
	case reflect.Struct:
		structName := typ.Name()
		if prefix == "" {
			logger.Debug().Msgf("Struct: %s", structName)
		} else {
			logger.Debug().Msgf("%s: %s {", prefix, structName)
		}

		// Iterate over struct fields
		for i := 0; i < val.NumField(); i++ {
			field := typ.Field(i)
			fieldVal := val.Field(i)

			// Skip unexported fields
			if !fieldVal.CanInterface() {
				continue
			}

			fieldPrefix := field.Name
			if prefix != "" {
				fieldPrefix = prefix + "." + field.Name
			}

			s.dumpValue(logger, fieldVal.Interface(), fieldPrefix, visited, depth+1)
		}

		if prefix != "" {
			logger.Debug().Msgf("%s: }", prefix)
		}

	case reflect.Map:
		logger.Debug().Msgf("%s: map[%s]%s (len: %d) {",
			prefix, typ.Key().String(), typ.Elem().String(), val.Len())

		iter := val.MapRange()
		for iter.Next() {
			k := iter.Key()
			vv := iter.Value()

			keyStr := fmt.Sprintf("%v", k.Interface())
			mapPrefix := prefix + "[" + keyStr + "]"

			s.dumpValue(logger, vv.Interface(), mapPrefix, visited, depth+1)
		}

		logger.Debug().Msgf("%s: }", prefix)

	case reflect.Slice, reflect.Array:
		logger.Debug().Msgf("%s: %s (len: %d, cap: %d) {",
			prefix, typ.String(), val.Len(), val.Cap())

		// Limit the number of elements to log for large slices/arrays
		maxElements := 10
		for i := 0; i < val.Len() && i < maxElements; i++ {
			elemPrefix := fmt.Sprintf("%s[%d]", prefix, i)
			elem := val.Index(i)
			// If the element is addressable/pointer, pass its Interface
			if elem.CanInterface() {
				s.dumpValue(logger, elem.Interface(), elemPrefix, visited, depth+1)
			} else {
				// fallback for unexported/unaligned values
				s.dumpValue(logger, reflect.New(elem.Type()).Elem().Interface(), elemPrefix, visited, depth+1)
			}
		}

		if val.Len() > maxElements {
			logger.Debug().Msgf("%s: ... (%d more elements)", prefix, val.Len()-maxElements)
		}

		logger.Debug().Msgf("%s: }", prefix)

	default:
		// For basic types, log the current reflect.Value's interface
		if val.IsValid() && val.CanInterface() {
			logger.Debug().Msgf("%s: %v", prefix, val.Interface())
		} else {
			logger.Debug().Msgf("%s: %v", prefix, v)
		}
	}
}
