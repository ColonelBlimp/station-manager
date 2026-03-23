# Station Manager: IoC/DI package

A small, reflection-based dependency injection container for Go. It lets you:
- Register beans by ID either by type (structs normalized to pointers) or by instance
- Discover dependencies from struct fields tagged with `di.inject:"<id>"`
- Build the container to instantiate and inject dependencies
- Optionally source string literals (like config paths) via a global LiteralProvider
- Resolve beans safely at runtime, including a generic helper `ResolveAs[T]`

This project is intentionally minimal and aims to be straightforward to read and extend.

## Installation

- Go 1.20+
- Add to your project:
  go get github.com/ColonelBlimp/iocdi

## Quick start

1) Define your types and tag the fields you want injected using `di.inject`.

```
    type Service struct {
        Config *Config `di.inject:"ServiceBeanConfig"`
        Logger *Logger `di.inject:"ServiceBeanLogger"`
    }

    type Config struct {
        WorkingDir string `di.inject:"WorkingDir"`
    }

    type Logger struct{ _ byte } // non-zero sized to ensure distinct instances
```

2) Register beans and build the container.

```
    c := iocdi.New()
    _ = c.Register("ServiceBean", reflect.TypeOf((*Service)(nil)))
    _ = c.RegisterInstance("servicebeanconfig", &Config{})
    _ = c.RegisterInstance("ServiceBeanLogger", &Logger{})

    // You can register simple values as instances:
    _ = c.RegisterInstance("workingdir", "/var/app")

    // Finalize (idempotent). Build can also be triggered automatically by ResolveSafe.
    if err := c.Build(); err != nil { panic(err) }
```

3) Resolve your service.

```
    v, err := c.ResolveSafe("bervicebean")
    if err != nil { panic(err) }
    svc := v.(*Service)
    // svc.Config and svc.Logger are injected; svc.Config.WorkingDir == "/var/app"
```

### Generic helper: ResolveAs

Use the generic helper for type-safe resolution:

```
    logger, err := iocdi.ResolveAs[*Logger](c, "Servicebeanlogger")
    if err != nil { /* handle */ }
```

## LiteralProvider for strings

You can provide string dependencies at injection time without pre-registering them via a global hook:

```
    iocdi.SetLiteralProvider(func(id string, t reflect.Type) (any, bool, error) {
        if id == "WorkingDir" { return "/workspace", true, nil }
        return nil, false, nil
    })
```

When `Build()` runs and encounters a missing string dependency (e.g., `WorkingDir`), the container will query the provider and inject the returned value. If you later register a bean with the same ID, that takes precedence and the provider is not called.

Note: The LiteralProvider is intended for strings only. You can extend the approach if you need more scalar types.

## Registration rules

- Register(type): supports struct or pointer-to-struct types; simple kinds (e.g., string) are not supported here
- RegisterInstance(id, value): supports any value; struct values are normalized to pointers for consistent injection
- Field injection is explicit: only exported fields with the `di.inject` tag are considered
- Supported dependency field types:
  - Pointer-to-structs (e.g., `*Config`)
  - string (optionally fulfilled by LiteralProvider)

## Build, resolve, and lifecycle

- Build is idempotent and populates any missing struct instances
- Registration is closed after a successful Build
- ResolveSafe ensures Build is called on first use; Resolve panics on errors (prefer ResolveSafe)

## Cycle detection

The container performs DFS-based cycle detection and returns a descriptive error path (e.g., `A -> B -> A`).

## Concurrency notes

- Build is guarded; registration and build use internal locking
- Resolution after build uses read locks for safety
- The global LiteralProvider is stored via atomic.Value for race-free reads and safe updates; set it before building to avoid surprises

## Limitations (by design)

- Only pointer-to-struct and string fields are discovered for injection
- Interfaces and non-pointer struct fields are not supported by the built-in discovery
- Tag-only injection: untagged fields are ignored, even if a compatible bean exists

## Testing

Run tests with:
  go test ./...

The suite includes injection scenarios, literal provider behavior, cycle detection, and Resolve/ResolveAs coverage.

## License

MIT.

