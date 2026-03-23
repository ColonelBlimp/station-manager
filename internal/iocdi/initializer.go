package iocdi

// Initializer is an optional interface that a bean may implement to perform
// additional initialization after all of its dependencies have been injected.
//
// Beans implementing this interface must define Initialize() error. The
// container will call Initialize() during Build(), after dependency injection
// has completed. If Initialize returns an error, Build() will fail with that
// error.
//
// Note: This interface is intentionally defined in the root iocdi package with
// no imports and no references to internal container types to avoid introducing
// cyclic dependencies when implemented by beans in other modules/packages.
type Initializer interface {
	Initialize() error
}
