package iocdi

import (
	"fmt"
	"reflect"
	"strings"
)

// checkForDependency analyzes the provided beanType for any tagged dependencies and registers as a required dependency.
// It processes exported fields ONLY with the `di.inject` tag, identifying dependencies to be resolved later.
// Handles pointer-to-struct fields and string fields, storing them in the requiredDependency map.
// Non-struct types or unexported fields are ignored during this process.
// Returns true if any dependencies were found, false otherwise.
func (c *Container) checkForDependency(beanType reflect.Type) (bool, []string) {
	// Check if the bean is a pointer to a struct or a struct
	if beanType.Kind() == reflect.Ptr {
		if beanType.Elem().Kind() != reflect.Struct {
			return false, nil
		}
	} else if beanType.Kind() != reflect.Struct {
		return false, nil
	}

	dependencyIDs := make([]string, 0)
	hasDependencies := false
	beanTypeElement := beanType.Elem()
	// Iterate through the fields of the struct and check for the `di.inject` tag
	// if seen, add it to the required list
	for i := 0; i < beanTypeElement.NumField(); i++ {
		field := beanTypeElement.Field(i)
		tagName, exists := field.Tag.Lookup(string(inject))
		tagName = strings.ToLower(tagName) // Enfore lower-case tag names
		if !exists {
			continue
		}

		// We only support exported fields, otherwise it requires the use of unsafe pointers.
		if field.IsExported() {
			// Only handle pointer-to-struct fields for injection demo
			if field.Type.Kind() == reflect.Ptr && field.Type.Elem().Kind() == reflect.Struct {
				c.requiredDependency[tagName] = field.Type.Elem()
				hasDependencies = true
				dependencyIDs = append(dependencyIDs, tagName)
			}

			// string-typed fields
			if field.Type.Kind() == reflect.String {
				c.requiredDependency[tagName] = field.Type
				hasDependencies = true
				dependencyIDs = append(dependencyIDs, tagName)
			}

			// interface-typed fields
			if field.Type.Kind() == reflect.Interface {
				c.requiredDependency[tagName] = field.Type
				hasDependencies = true
				dependencyIDs = append(dependencyIDs, tagName)
			}
		}
	}

	return hasDependencies, dependencyIDs
}

func (c *Container) injectDependencies() error {
	//	fmt.Println("Injecting dependencies...")

	// DFS-based cycle detection and ordered injection
	visited := make(map[string]bool) // fully processed
	onPath := make(map[string]bool)  // nodes in the current recursion stack
	path := make([]string, 0, 16)    // ordered path for clear errors

	var joinPath = func(p []string, last string) string {
		s := emptyString
		for i, v := range p {
			if i > 0 {
				s += pathSep
			}
			s += v
		}
		if last != emptyString {
			if len(s) > 0 {
				s += pathSep // " -> "
			}
			s += last
		}
		return s
	}

	var visit func(id string) error
	visit = func(id string) error {
		// Unknown bean (should not happen here; callers ensure registration)
		bn, ok := c.registeredBeans[id]
		if !ok {
			return fmt.Errorf("injectDependencies: receiver bean '%s' not found", id)
		}

		// Cycle checks
		if onPath[id] {
			// Produce a cycle path ending back at id
			return fmt.Errorf("dependency cycle detected: %s", joinPath(path, id))
		}
		if visited[id] {
			return nil
		}

		// Enter node
		onPath[id] = true
		path = append(path, id)

		if bn.hasDependencies {
			//			fmt.Println("Injecting dependencies for bean:", bn.id, " hasDependencies:", bn.hasDependencies, "list:", bn.dependencies)

			if bn.instance == nil {
				return fmt.Errorf("injectDependencies: receiver bean '%s' is nil", bn.id)
			}

			for _, depBeanID := range bn.dependencies {
				depBean, ok := c.registeredBeans[depBeanID]
				if !ok {
					// Attempt to resolve via literalProvider if the expected type is known and is string
					if expectedType, okType := c.requiredDependency[depBeanID]; okType && expectedType.Kind() == reflect.String {
						if lp := loadLiteralProvider(); lp != nil {
							if val, found, err := lp(depBeanID, expectedType); err != nil {
								return fmt.Errorf("injectDependencies: literal provider error for '%s': %w", depBeanID, err)
							} else if found {
								// Synthesize a bean from the literal so downstream code can proceed uniformly
								depBean = bean{
									id:       depBeanID,
									instance: val,
									beanType: expectedType,
									// keep other fields default (no dependencies, etc.)
								}
								c.registeredBeans[depBeanID] = depBean
								ok = true
							}
						}
					}
					if !ok {
						return fmt.Errorf("injectDependencies: dependency bean '%s' for '%s' receiver bean not found", depBeanID, bn.id)
					}
				}

				// Recurse into dependency first to detect indirect cycles and ensure its deps are injected
				if err := visit(depBeanID); err != nil {
					return err
				}

				// Ensure the instance exists before injection
				if depBean.instance == nil {
					return fmt.Errorf("injectDependencies: dependency bean '%s' for '%s' receiver bean not instantiated", depBeanID, bn.id)
				}

				// Inject depBean into receiver bn; pass current path for direct/self-cycle guard and clarity
				if err := injectIntoStruct(bn, depBean, append([]string{}, path...)); err != nil {
					return fmt.Errorf("injectDependencies: %w", err)
				}

				// Reload potentially updated receiver from map (in case injectIntoStruct updated anything)
				bn = c.registeredBeans[id]
			}
		}

		// Leave node
		onPath[id] = false
		path = path[:len(path)-1]
		visited[id] = true
		return nil
	}

	// Visit all registered beans
	for id := range c.registeredBeans {
		if err := visit(id); err != nil {
			return err
		}
	}

	return nil
}
