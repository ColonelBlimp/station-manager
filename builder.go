package adapters

import "reflect"

// Builder provides a fluent API to construct an Adapter with options, converters and validators pre-registered.
type Builder struct {
	opts     []Option
	convsG   map[string]ConverterFunc
	convsDst map[reflect.Type]map[string]ConverterFunc
	convsP   map[[2]reflect.Type]map[string]ConverterFunc
	valsG    map[string]ValidatorFunc
	valsDst  map[reflect.Type]map[string]ValidatorFunc
	valsP    map[[2]reflect.Type]map[string]ValidatorFunc
}

// NewBuilder creates a new builder.
func NewBuilder() *Builder {
	return &Builder{
		convsG:   make(map[string]ConverterFunc),
		convsDst: make(map[reflect.Type]map[string]ConverterFunc),
		convsP:   make(map[[2]reflect.Type]map[string]ConverterFunc),
		valsG:    make(map[string]ValidatorFunc),
		valsDst:  make(map[reflect.Type]map[string]ValidatorFunc),
		valsP:    make(map[[2]reflect.Type]map[string]ValidatorFunc),
	}
}

// WithOptions appends adapter options to the builder.
func (b *Builder) WithOptions(opts ...Option) *Builder { b.opts = append(b.opts, opts...); return b }

// AddConverter registers a global converter by field name.
func (b *Builder) AddConverter(field string, fn ConverterFunc) *Builder {
	b.convsG[field] = fn
	return b
}

// AddConverterFor registers a converter for a destination type and field name.
func (b *Builder) AddConverterFor(dst any, field string, fn ConverterFunc) *Builder {
	dt := reflect.TypeOf(dst)
	if dt.Kind() == reflect.Ptr {
		dt = dt.Elem()
	}
	m := b.convsDst[dt]
	if m == nil {
		m = make(map[string]ConverterFunc)
		b.convsDst[dt] = m
	}
	m[field] = fn
	return b
}

// AddConverterForPair registers a converter for a (src,dst) pair and field name.
func (b *Builder) AddConverterForPair(src, dst any, field string, fn ConverterFunc) *Builder {
	st := reflect.TypeOf(src)
	if st.Kind() == reflect.Ptr {
		st = st.Elem()
	}
	dt := reflect.TypeOf(dst)
	if dt.Kind() == reflect.Ptr {
		dt = dt.Elem()
	}
	key := [2]reflect.Type{st, dt}
	m := b.convsP[key]
	if m == nil {
		m = make(map[string]ConverterFunc)
		b.convsP[key] = m
	}
	m[field] = fn
	return b
}

// AddValidator registers a global validator by field name.
func (b *Builder) AddValidator(field string, fn ValidatorFunc) *Builder {
	b.valsG[field] = fn
	return b
}

// AddValidatorFor registers a validator for a destination type and field name.
func (b *Builder) AddValidatorFor(dst any, field string, fn ValidatorFunc) *Builder {
	dt := reflect.TypeOf(dst)
	if dt.Kind() == reflect.Ptr {
		dt = dt.Elem()
	}
	m := b.valsDst[dt]
	if m == nil {
		m = make(map[string]ValidatorFunc)
		b.valsDst[dt] = m
	}
	m[field] = fn
	return b
}

// AddValidatorForPair registers a validator for a (src,dst) pair and field name.
func (b *Builder) AddValidatorForPair(src, dst any, field string, fn ValidatorFunc) *Builder {
	st := reflect.TypeOf(src)
	if st.Kind() == reflect.Ptr {
		st = st.Elem()
	}
	dt := reflect.TypeOf(dst)
	if dt.Kind() == reflect.Ptr {
		dt = dt.Elem()
	}
	key := [2]reflect.Type{st, dt}
	m := b.valsP[key]
	if m == nil {
		m = make(map[string]ValidatorFunc)
		b.valsP[key] = m
	}
	m[field] = fn
	return b
}

// Build constructs an Adapter using a single registry swap for converters and validators.
func (b *Builder) Build() *Adapter {
	a := NewWithOptions(b.opts...)
	// Seed registries in one shot to avoid many copy-on-write swaps.
	creg := &converterRegistry{global: make(map[string]ConverterFunc, len(b.convsG)), byDst: make(map[reflect.Type]map[string]ConverterFunc, len(b.convsDst)), byPair: make(map[[2]reflect.Type]map[string]ConverterFunc, len(b.convsP))}
	for k, v := range b.convsG {
		creg.global[k] = v
	}
	for t, m := range b.convsDst {
		sub := make(map[string]ConverterFunc, len(m))
		for k, v := range m {
			sub[k] = v
		}
		creg.byDst[t] = sub
	}
	for k, m := range b.convsP {
		sub := make(map[string]ConverterFunc, len(m))
		for fk, fv := range m {
			sub[fk] = fv
		}
		creg.byPair[k] = sub
	}
	a.converters.Store(creg)
	vreg := &validatorRegistry{global: make(map[string]ValidatorFunc, len(b.valsG)), byDst: make(map[reflect.Type]map[string]ValidatorFunc, len(b.valsDst)), byPair: make(map[[2]reflect.Type]map[string]ValidatorFunc, len(b.valsP))}
	for k, v := range b.valsG {
		vreg.global[k] = v
	}
	for t, m := range b.valsDst {
		sub := make(map[string]ValidatorFunc, len(m))
		for k, v := range m {
			sub[k] = v
		}
		vreg.byDst[t] = sub
	}
	for k, m := range b.valsP {
		sub := make(map[string]ValidatorFunc, len(m))
		for fk, fv := range m {
			sub[fk] = fv
		}
		vreg.byPair[k] = sub
	}
	a.validators.Store(vreg)
	return a
}
