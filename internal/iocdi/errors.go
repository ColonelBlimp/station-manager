package iocdi

import "errors"

var (
	ErrBeanIdParamIsEmpty   = errors.New("beanID parameter is empty")
	ErrBeanTypeParamIsNil   = errors.New("beanType parameter is nil")
	ErrBeanParamIsNil       = errors.New("bean parameter is nil")
	ErrBeanTypeNotSupported = errors.New("beanType is not supported")
	ErrRegistrationClosed   = errors.New("container already built; registration is closed")
)
