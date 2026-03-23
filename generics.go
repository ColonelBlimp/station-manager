package adapters

// Generic helpers as top-level functions (methods cannot have type parameters yet)

func Copy[T any](a *Adapter, dst *T, src any) error { return a.Into(dst, src) }

func AdaptTo[T any](a *Adapter, src any) (*T, error) {
	var d T
	if err := a.Into(&d, src); err != nil {
		return nil, err
	}
	return &d, nil
}

func Make[T any](a *Adapter, src any) (T, error) {
	var d T
	err := a.Into(&d, src)
	return d, err
}
