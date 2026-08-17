package iocdi

import "errors"

var (
	ErrBeanIdParamIsEmpty   = errors.New("beanID parameter is empty")
	ErrBeanTypeParamIsNil   = errors.New("beanType parameter is nil")
	ErrBeanParamIsNil       = errors.New("bean parameter is nil")
	ErrBeanTypeNotSupported = errors.New("beanType is not supported")
	ErrRegistrationClosed   = errors.New("container already built; registration is closed")
	// ErrDuplicateBeanID is returned when a bean ID is registered twice. Bean IDs
	// are unique; a duplicate (e.g. a ServiceName typo in startup wiring) is
	// rejected AT the registration call rather than silently overwriting the
	// earlier bean and failing far away later (review 2026-06-19 M2).
	ErrDuplicateBeanID = errors.New("bean ID already registered")

	// Lifecycle-node registry (ADR 0070 / docs/v2-design/lifecycle.md §3.1). The plan is the
	// single source of truth for shutdown ordering; every one of these makes the daemon refuse
	// to start rather than run an ill-formed graph.
	ErrEmptyNodeID        = errors.New("lifecycle node ID is empty")
	ErrDuplicateNodeID    = errors.New("lifecycle node ID already registered")
	ErrUnknownNodeDep     = errors.New("lifecycle node references an unknown node")
	ErrStartCycle         = errors.New("lifecycle start graph has a cycle")
	ErrDrainCycle         = errors.New("lifecycle drain graph has a cycle")
	ErrMultipleRFCritical = errors.New("more than one RFCritical lifecycle node")
	ErrPlanFrozen         = errors.New("lifecycle plan already built; node registration is closed")

	// ErrAlreadyInitialized enforces the single-init-owner guardrail (ADR 0070): a container uses
	// either explicit Build() OR orchestrator-owned initialization, never both.
	ErrAlreadyInitialized = errors.New("container initialization already claimed by another owner")
)
