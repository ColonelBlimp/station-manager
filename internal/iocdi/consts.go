package iocdi

const (
	emptyString = ""
	pathSep     = " -> "
)

type tag string

const (
	inject tag = "di.inject" // di.inject is the default tag for constructor injection. The field MUST be exported.
)
