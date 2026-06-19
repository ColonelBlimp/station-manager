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
)
