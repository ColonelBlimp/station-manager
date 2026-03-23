# Station Manager: types package

This package contains the Go types used across the various modules and applications which comprise the *Station Manager*
application suite.

It is a core module that defines the data structures and types that are shared and, by design, **does not/cannot depend
on any other **Station Manager package**; only Go system modules (e.g. "time") are allowed. It is essential that this
module **HAS NO DEPENDENCIES** from *Station Manager* to ensure that it can be used in anywhere without causing cyclic
dependency errors.

Candidates for inclusion in this module are:
1. Go data structures and types that are shared across modules/packages
2. Constants that are used across modules/packages
3. Interfaces that are implemented by multiple modules/packages

