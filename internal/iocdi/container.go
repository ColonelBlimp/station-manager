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
	// Lifecycle flags (ADR 0070 Wire()/Build() split). wired = beans constructed + injected, not
	// yet Initialized; built = wired AND all Initializers run (the compat path); initialized =
	// SOME init path has run, the single-init-owner guardrail marker (Build claims it, and the
	// orchestrator will too, so the two can never both initialize).
	built       atomic.Bool
	wired       atomic.Bool
	initialized atomic.Bool

	// requiredDependency maps bean identifiers to their corresponding reflect.Type, identifying dependencies
	// required by registered beans. For example, if `Service` has a dependency on `Config`, then `Config` will be
	// added to the requiredDependency list.
	requiredDependency map[string]reflect.Type

	// registeredBeans stores all registered beans mapped by their unique string identifiers.
	// This is the source of truth for all beans.
	registeredBeans map[string]bean

	// lifecycleNodes is the ADR-0070 lifecycle-node registry — independent of bean registration,
	// in registration order (the deterministic shutdown tiebreak), guarded by regMu. planFrozen is
	// set by Plan() and CLOSES both node AND bean registration: the plan snapshots the DI graph
	// (registeredBeans) to derive start edges, so a bean registered after Plan would leave the
	// "immutable" plan missing an edge (codex P1). It is atomic so Register can check it cheaply.
	// See lifecycle_nodes.go.
	lifecycleNodes []Node
	planFrozen     atomic.Bool
}

func New() *Container {
	return &Container{
		requiredDependency: make(map[string]reflect.Type),
		registeredBeans:    make(map[string]bean),
	}
}

// Register registers a bean by its reflect.Type.
// If the type is a struct, it will be normalized to a pointer-to-struct for consistent injection semantics.
// The 'beanID' parameter is case-INSENSITIVE: it is lower-cased here and tags are
// lower-cased before matching, so "Foo" and "foo" are the same bean.
//
// This method only supports structs and POINTERS-TO-STRUCT; a pointer to a
// non-struct (e.g. *int) or any other simple type must be registered as an
// instance via RegisterInstance. A bean ID may be registered only once —
// registering it twice returns ErrDuplicateBeanID.
func (c *Container) Register(beanID string, beanType reflect.Type) error {
	if beanID == emptyString {
		return ErrBeanIdParamIsEmpty
	}
	if beanType == nil {
		return ErrBeanTypeParamIsNil
	}
	if c.built.Load() || c.planFrozen.Load() {
		return ErrRegistrationClosed
	}

	beanID = strings.ToLower(beanID)

	// Normalize struct kind to pointer-to-struct. A pointer is accepted only if it
	// points to a struct — Build only instantiates pointer-to-struct beans, so a
	// *int (etc.) would register silently and then surface as "not initialized" at
	// ResolveSafe, far from the bad Register call (review 2026-06-19 M2).
	switch beanType.Kind() {
	case reflect.Ptr:
		if beanType.Elem().Kind() != reflect.Struct {
			return ErrBeanTypeNotSupported
		}
	case reflect.Struct:
		beanType = reflect.PointerTo(beanType)
	default:
		// For non-struct simple types (e.g., string) this registration style is not supported.
		// Use RegisterInstance for simple literals instead.
		return ErrBeanTypeNotSupported
	}

	if beanRegisterPreLockForTest != nil {
		beanRegisterPreLockForTest()
	}
	c.regMu.Lock()
	defer c.regMu.Unlock()
	// Every shared-state mutation happens AFTER the under-lock freeze recheck (codex P1 ×2): Plan()
	// sets planFrozen AND snapshots the DI graph under this same lock, and checkForDependency mutates
	// requiredDependency — so a rejected registration must touch NEITHER registeredBeans NOR
	// requiredDependency, or it leaves a phantom required-dep that breaks a later Build.
	if c.planFrozen.Load() {
		return ErrRegistrationClosed
	}
	if _, dup := c.registeredBeans[beanID]; dup {
		return ErrDuplicateBeanID
	}
	hasDeps, deps := c.checkForDependency(beanType)
	c.registeredBeans[beanID] = bean{
		id:              beanID,
		beanType:        beanType,
		instance:        nil, // instance will be created during Build
		singleton:       false,
		hasDependencies: hasDeps,
		dependencies:    deps,
	}
	return nil
}

// RegisterInstance registers a concrete instance for type T.
// The instance is treated as a singleton. Struct instances are normalized to pointers.
// The 'beanID' parameter is case-INSENSITIVE (lower-cased here, as tags are before
// matching). A bean ID may be registered only once — a duplicate returns
// ErrDuplicateBeanID rather than silently replacing the earlier instance.
func (c *Container) RegisterInstance(beanID string, instance any) error {
	if beanID == emptyString {
		return ErrBeanIdParamIsEmpty
	}
	if instance == nil {
		return ErrBeanParamIsNil
	}
	if c.built.Load() || c.planFrozen.Load() {
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

	if beanRegisterPreLockForTest != nil {
		beanRegisterPreLockForTest()
	}
	c.regMu.Lock()
	defer c.regMu.Unlock()
	// Mutation only after the under-lock freeze recheck + dup check (codex P1 ×2) — see Register.
	if c.planFrozen.Load() {
		return ErrRegistrationClosed
	}
	if _, dup := c.registeredBeans[beanID]; dup {
		return ErrDuplicateBeanID
	}
	has, deps := c.checkForDependency(beanType)
	c.registeredBeans[beanID] = bean{
		id:              beanID,
		beanType:        beanType,
		instance:        instance,
		singleton:       true,
		hasDependencies: has,
		dependencies:    deps,
	}
	return nil
}

// Wire constructs and injects all registered beans in dependency order but does NOT run
// Initializer.Initialize — the orchestrator (ADR 0070) owns initialization. Idempotent: a no-op
// once wired or built.
func (c *Container) Wire() (err error) {
	c.buildLock.Lock()
	defer c.buildLock.Unlock()
	if c.wired.Load() || c.built.Load() {
		return nil
	}
	c.regMu.Lock()
	defer func() {
		if err == nil {
			c.wired.Store(true)
		}
		c.regMu.Unlock()
	}()
	return c.wireLocked()
}

// Build wires the container and then runs every Initializer in dependency order — the EXPLICIT
// initialization path for callers (import/restore) that want fully-built beans without the
// orchestrator. Idempotent. A container uses either Build() OR orchestrator-owned initialization,
// NEVER both (single-init-owner guardrail): Build refuses with ErrAlreadyInitialized if
// initialization has already been claimed elsewhere.
func (c *Container) Build() (err error) {
	c.buildLock.Lock()
	defer c.buildLock.Unlock()
	if c.built.Load() {
		return nil
	}
	c.regMu.Lock()
	defer func() {
		if err == nil {
			c.built.Store(true)
		}
		c.regMu.Unlock()
	}()
	if !c.wired.Load() {
		if err = c.wireLocked(); err != nil {
			return err
		}
		c.wired.Store(true)
	}
	// Single-init-owner guardrail: claim initialization; fail if the orchestrator already did.
	if !c.initialized.CompareAndSwap(false, true) {
		return ErrAlreadyInitialized
	}
	return c.initializeAllLocked()
}

// wireLocked runs the precheck + instantiate + inject phases. Caller holds buildLock and regMu.
func (c *Container) wireLocked() error {
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
			instance, ierr := createInstance(bn.beanType)
			if ierr != nil {
				return ierr
			}
			bn.instance = instance
			bn.singleton = true
			c.registeredBeans[bn.id] = bn
		}
	}

	// Inject dependencies.
	return c.injectDependencies()
}

// initializeAllLocked runs every Initializer in dependency order (Build's phase), via a DFS
// topological traversal over the registration-time dependency edges. Caller holds buildLock and regMu.
func (c *Container) initializeAllLocked() error {
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

	return nil
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

// ResolveSafe returns a bean instance by its ID. It ensures the container is WIRED (constructed +
// injected) before resolving — NOT initialized (ADR 0070): resolution never forces Initialize, so a
// stray resolve after the orchestrator has taken over init can't double-initialize. Callers that
// need fully-initialized beans (import/restore) call Build() explicitly first.
func (c *Container) ResolveSafe(beanID string) (any, error) {
	if beanID == emptyString {
		return nil, ErrBeanIdParamIsEmpty
	}

	beanID = strings.ToLower(beanID)

	// Ensure the container is wired before resolving (wire-only, never Build).
	if !c.wired.Load() && !c.built.Load() {
		if err := c.Wire(); err != nil {
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
