package iocdi

import (
	"fmt"
	"reflect"
	"strings"
)

func createInstance(beanType reflect.Type) (any, error) {
	if beanType.Kind() == reflect.Ptr {
		return reflect.New(beanType.Elem()).Interface(), nil
	}
	// Support direct struct kinds by creating a pointer to it,
	// so all created instances are pointers for consistency.
	if beanType.Kind() == reflect.Struct {
		return reflect.New(beanType).Interface(), nil
	}
	return nil, fmt.Errorf("beanType is not supported: %v", beanType.Kind())
}

func injectIntoStruct(receiverBean bean, depBean bean, chain []string) error {
	// Fail fast if a direct/self cycle is observed based on the current chain context.
	// This complements the DFS detection in injectDependencies with a local guard.
	for _, id := range chain {
		if id == depBean.id {
			return fmt.Errorf("dependency cycle detected: %s -> %s", strings.Join(chain, pathSep), depBean.id)
		}
	}

	rv := reflect.ValueOf(receiverBean.instance)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("injectIntoStruct: receiver bean '%s' is not a struct", receiverBean.id)
	}

	depVal := reflect.ValueOf(depBean.instance)
	depType := depBean.beanType

	// Iterate exported fields and inject only when the tag matches the dependency id.
	for i := 0; i < rv.NumField(); i++ {
		sf := rv.Type().Field(i)
		// Honor tag usage: only consider fields with di.inject tag matching the dep bean id.
		// Normalize tag to lowercase to align with the container's lowercase bean ID policy.
		tagVal := sf.Tag.Get(string(inject))
		if tagVal == emptyString {
			continue
		}
		tagVal = strings.ToLower(tagVal)
		if tagVal != depBean.id {
			continue
		}

		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}

		fieldType := fv.Type()

		// Exact type match, including basic types like string and exact pointer types
		if fieldType == depType {
			// Special-case: if this is a pointer to an empty struct, allocate a fresh instance to
			// avoid identical pointer values for zero-sized types (ensures distinct injections like LoggerA vs LoggerB).
			if depType.Kind() == reflect.Ptr && depType.Elem().Kind() == reflect.Struct && depType.Elem().NumField() == 0 {
				fv.Set(reflect.New(depType.Elem()))
			} else {
				fv.Set(depVal)
			}
			continue
		}

		// field is interface, dependency implements it
		if fieldType.Kind() == reflect.Interface {
			// Use depVal.Type() instead of depType in case instance is a more specific concrete type
			if depVal.Type().Implements(fieldType) {
				fv.Set(depVal)
			}
			continue
		}

		// Normalize pointer/value combinations:
		// field: *T, dep: T
		if fieldType.Kind() == reflect.Ptr && depType.Kind() == reflect.Struct && fieldType.Elem() == depType {
			ptr := reflect.New(depType)
			ptr.Elem().Set(depVal)
			fv.Set(ptr)
			continue
		}

		// field: T, dep: *T
		if fieldType.Kind() == reflect.Struct && depType.Kind() == reflect.Ptr && depType.Elem() == fieldType {
			fv.Set(depVal.Elem())
			continue
		}

		// field: *T, dep: *T with same element types
		if fieldType.Kind() == reflect.Ptr && depType.Kind() == reflect.Ptr && fieldType.Elem() == depType.Elem() {
			fv.Set(depVal)
			continue
		}

		// If we reach here, types are incompatible; leave field untouched (explicit tag ensures we don't match by type alone).
	}

	return nil
}
