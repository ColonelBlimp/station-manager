package iocdi

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
)

type bean struct {
	id              string
	beanType        reflect.Type
	instance        any
	singleton       bool
	hasDependencies bool
	dependencies    []string
}

type Container struct {
	buildLock sync.Mutex
	// Protects access to registeredBeans and requiredDependency during registration/build.
	regMu sync.RWMutex
	// Indicates whether the container has been built/finalized.
	built atomic.Bool

	// requiredDependency maps bean identifiers to their corresponding reflect.Type, identifying dependencies
	// required by registered beans. For example, if `Service` has a dependency on `Config`, then `Config` will be
	// added to the requiredDependency list.
	requiredDependency map[string]reflect.Type

	// registeredBeans stores all registered beans mapped by their unique string identifiers.
	// This is the source of truth for all beans.
	registeredBeans map[string]bean
}

func New() *Container {
	return &Container{
		requiredDependency: make(map[string]reflect.Type),
		registeredBeans:    make(map[string]bean),
	}
}

// Register registers a bean by its reflect.Type.
// If the type is a struct, it will be normalized to a pointer-to-struct for consistent injection semantics.
// The 'beanID' parameter is case-sensitive with regard to the bean identifier and the
// coresponding receiving bean tag. The case of the bean identifier must match the case of the
// tag in the receiving bean.
//
// This method only supports registering structs and pointers to structs; simple types (e.g., string)
// must be registered as instances using RegisterInstance.
func (c *Container) Register(beanID string, beanType reflect.Type) error {
	if beanID == emptyString {
		return ErrBeanIdParamIsEmpty
	}
	if beanType == nil {
		return ErrBeanTypeParamIsNil
	}
	if c.built.Load() {
		return ErrRegistrationClosed
	}

	beanID = strings.ToLower(beanID)

	// Normalize struct kind to pointer-to-struct
	switch beanType.Kind() {
	case reflect.Ptr:
		// Expect a pointer to struct (legacy rule)
	case reflect.Struct:
		beanType = reflect.PointerTo(beanType)
	default:
		// For non-struct simple types (e.g., string) this registration style is not supported.
		// Use RegisterInstance for simple literals instead.
		return ErrBeanTypeNotSupported
	}

	hasDeps, deps := c.checkForDependency(beanType)
	b := bean{
		id:              beanID,
		beanType:        beanType,
		instance:        nil, // instance will be created during Build
		singleton:       false,
		hasDependencies: hasDeps,
		dependencies:    deps,
	}
	c.regMu.Lock()
	c.registeredBeans[beanID] = b
	c.regMu.Unlock()
	return nil
}

// RegisterInstance registers a concrete instance for type T.
// The instance is treated as a singleton. Struct instances are normalized to pointers.
// The 'beanID' parameter is case-sensitive with regard to the bean identifier and the
// coresponding receiving bean tag. The case of the bean identifier must match the case of the
// tag in the receiving bean.
func (c *Container) RegisterInstance(beanID string, instance any) error {
	if beanID == emptyString {
		return ErrBeanIdParamIsEmpty
	}
	if instance == nil {
		return ErrBeanParamIsNil
	}
	if c.built.Load() {
		return ErrRegistrationClosed
	}

	beanID = strings.ToLower(beanID) // Enforce lower-case bean identifiers

	beanType := reflect.TypeOf(instance)

	// Normalize struct instances to pointers for consistent type comparisons and injection behavior.
	// This ensures pointer-typed fields can be injected even if the user registered a struct value.
	if beanType.Kind() == reflect.Struct {
		ptr := reflect.New(beanType)
		ptr.Elem().Set(reflect.ValueOf(instance))
		instance = ptr.Interface()
		beanType = ptr.Type()
	}

	has, deps := c.checkForDependency(beanType)
	b := bean{
		id:              beanID,
		beanType:        beanType,
		instance:        instance,
		singleton:       true,
		hasDependencies: has,
		dependencies:    deps,
	}

	c.regMu.Lock()
	c.registeredBeans[beanID] = b
	c.regMu.Unlock()

	return nil
}

// Build finalizes the container by verifying all required dependencies are registered,
// instantiating all registered beans, and injecting dependencies.
//
// If the container has already been built, this method is a no-op.
func (c *Container) Build() (err error) {
	c.buildLock.Lock()
	defer c.buildLock.Unlock()

	// Idempotent: if already built, nothing to do.
	if c.built.Load() {
		return nil
	}

	// All map reads/writes inside Build happen under regMu for safety against concurrent registration.
	c.regMu.Lock()
	defer func() {
		// Mark as built only on successful completion.
		if err == nil {
			c.built.Store(true)
		}
		c.regMu.Unlock()
	}()

	// First, check if the required dependencies have been registered
	// and there is type compatibility between the required dependency and the registered bean.
	for beanID, requiredType := range c.requiredDependency {
		regBean, ok := c.registeredBeans[beanID]
		if !ok {
			// Allow missing string dependencies to be provided by a LiteralProvider at injection time.
			if requiredType.Kind() == reflect.String {
				if lp := loadLiteralProvider(); lp != nil {
					// Defer resolution to injection; skip strict precheck for this dependency.
					continue
				}
			}
			return fmt.Errorf("bean `%s` is required but not registered", beanID)
		}

		registeredType := regBean.beanType
		compatible := false

		switch requiredType.Kind() {
		case reflect.Struct:
			// Require pointer to struct of exactly the same underlying type
			compatible = registeredType.Kind() == reflect.Ptr && registeredType.Elem() == requiredType
		case reflect.Interface:
			// allow concrete (typically pointer-to-struct) that implements the interface
			compatible = registeredType.Implements(requiredType)
		default:
			// Simple types (e.g., string) must match exactly
			compatible = registeredType == requiredType
		}

		if !compatible {
			return fmt.Errorf("bean '%s' type mismatch: required %v, registered %v", beanID, requiredType, registeredType)
		}
	}

	// The dependencies are all registered, so we can instantiate the beans
	for _, bn := range c.registeredBeans {
		if bn.instance != nil {
			continue // Already instantiated
		}

		if bn.beanType.Kind() == reflect.Ptr && bn.beanType.Elem().Kind() == reflect.Struct {
			//			fmt.Println("Creating instance of bean:", bn.id, "of type", bn.beanType)
			instance, ierr := createInstance(bn.beanType)
			if ierr != nil {
				return ierr
			}
			bn.instance = instance
			bn.singleton = true
			c.registeredBeans[bn.id] = bn
		}
	}

	// Inject dependencies
	if err = c.injectDependencies(); err != nil {
		return err
	}

	// Call Initializer on beans that implement it, after injection is complete
	// Ensure initializers run in dependency order: a bean's dependencies are initialized before the bean itself.
	// We perform a DFS topological traversal using the same dependency edges captured at registration time.
	visited := make(map[string]bool)
	onPath := make(map[string]bool)
	order := make([]string, 0, len(c.registeredBeans))

	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if onPath[id] {
			return fmt.Errorf("initializer order: dependency cycle detected at '%s'", id)
		}
		onPath[id] = true
		bn := c.registeredBeans[id]
		if bn.hasDependencies {
			for _, dep := range bn.dependencies {
				if _, ok := c.registeredBeans[dep]; !ok {
					return fmt.Errorf("initializer order: dependency '%s' required by '%s' not registered", dep, id)
				}
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		onPath[id] = false
		visited[id] = true
		order = append(order, id)
		return nil
	}

	for id := range c.registeredBeans {
		if err := visit(id); err != nil {
			return err
		}
	}

	for _, id := range order {
		bn := c.registeredBeans[id]
		if bn.instance == nil {
			continue
		}
		if initr, ok := bn.instance.(Initializer); ok {
			if ierr := initr.Initialize(); ierr != nil {
				return fmt.Errorf("initializer for bean '%s' failed: %w", id, ierr)
			}
		}
	}

	return err
}

// Resolve returns a bean instance by its ID or panics if it cannot be resolved.
// Prefer ResolveSafe in production code to handle errors gracefully.
func (c *Container) Resolve(beanID string) any {
	v, err := c.ResolveSafe(beanID)
	if err != nil {
		panic(err)
	}
	return v
}

// ResolveSafe returns a bean instance by its ID.
// It ensures the container is built before resolving and returns an error on failure.
func (c *Container) ResolveSafe(beanID string) (any, error) {
	if beanID == emptyString {
		return nil, ErrBeanIdParamIsEmpty
	}

	beanID = strings.ToLower(beanID)

	// Ensure the container is built before resolving.
	if !c.built.Load() {
		if err := c.Build(); err != nil {
			return nil, err
		}
	}

	// Look up the bean safely under read lock.
	c.regMu.RLock()
	bn, ok := c.registeredBeans[beanID]
	c.regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bean '%s' not found", beanID)
	}

	if bn.instance == nil {
		return nil, fmt.Errorf("bean '%s' is not initialized", beanID)
	}

	return bn.instance, nil
}

// ResolveAs returns a bean instance by its ID and casts it to type T.
// It ensures the container is built before resolving and returns an error on failure.
func ResolveAs[T any](c *Container, beanID string) (T, error) {
	v, err := c.ResolveSafe(beanID)
	if err != nil {
		var zero T
		return zero, err
	}
	x, ok := v.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("bean '%s' is not of requested type", beanID)
	}
	return x, nil
}
