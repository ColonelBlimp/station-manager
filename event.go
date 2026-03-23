package logging

import (
	"github.com/rs/zerolog"
	"net"
	"time"
)

// LogContext provides a fluent interface for building a context logger with pre-populated fields.
// Fields added through LogContext will be included in all subsequent log messages created from
// the resulting child logger. The returned LogContext is immutable; each method returns a new state.
type LogContext interface {
	Str(key, val string) LogContext
	Strs(key string, vals []string) LogContext
	Int(key string, val int) LogContext
	Int64(key string, val int64) LogContext
	Uint(key string, val uint) LogContext
	Uint64(key string, val uint64) LogContext
	Float64(key string, val float64) LogContext
	Bool(key string, val bool) LogContext
	Time(key string, val time.Time) LogContext
	Err(err error) LogContext
	Interface(key string, val interface{}) LogContext
	// Logger creates and returns the new context logger
	Logger() Logger
}

// LogEvent provides a fluent interface for structured logging with type-safe field methods.
// It wraps zerolog.Event to provide a clean API for adding typed fields to log entries.
// Calling Msg/Msgf/Send finalizes the event. If the event is a trackedLogEvent, finalizing
// the event also decrements the internal reference counters used for graceful shutdown.
type LogEvent interface {
	Str(key, val string) LogEvent
	Strs(key string, vals []string) LogEvent
	Stringer(key string, val interface{ String() string }) LogEvent
	Int(key string, val int) LogEvent
	Int8(key string, val int8) LogEvent
	Int16(key string, val int16) LogEvent
	Int32(key string, val int32) LogEvent
	Int64(key string, val int64) LogEvent
	Uint(key string, val uint) LogEvent
	Uint8(key string, val uint8) LogEvent
	Uint16(key string, val uint16) LogEvent
	Uint32(key string, val uint32) LogEvent
	Uint64(key string, val uint64) LogEvent
	Float32(key string, val float32) LogEvent
	Float64(key string, val float64) LogEvent
	Bool(key string, val bool) LogEvent
	Bools(key string, vals []bool) LogEvent
	Time(key string, val time.Time) LogEvent
	Dur(key string, val time.Duration) LogEvent
	// Err attaches an error and enriches the event with full chain fields
	// (error_chain, error_root, error_history, error_ops, error_root_op).
	Err(err error) LogEvent
	// AnErr attaches a named error and enriches the event with prefixed chain fields.
	AnErr(key string, err error) LogEvent
	Bytes(key string, val []byte) LogEvent
	Hex(key string, val []byte) LogEvent
	IPAddr(key string, val net.IP) LogEvent
	MACAddr(key string, val net.HardwareAddr) LogEvent
	Interface(key string, val interface{}) LogEvent
	Dict(key string, dict func(LogEvent)) LogEvent
	// Msg writes the event with a literal message
	Msg(msg string)
	// Msgf writes the event using a format string
	Msgf(format string, v ...interface{})
	// Send writes the event without a message
	Send()
}

// logEvent implements LogEvent by wrapping zerolog.Event
// It is safe to call methods on a nil underlying event; in that case the methods
// become no-ops. This allows returning a LogEvent even when the logger is disabled.
type logEvent struct {
	event *zerolog.Event
}

// trackedLogEvent wraps a logEvent and decrements the active operations counter when finalized.
// This ensures Close() can wait for in-flight logging to complete (up to a timeout) without races.
type trackedLogEvent struct {
	logEvent
	service  *Service
	location string // Debug: Track where this operation was created
}

// newLogEvent creates a new LogEvent wrapper.
// If e is nil, the returned LogEvent is a no-op implementation.
func newLogEvent(e *zerolog.Event) LogEvent {
	if e == nil {
		return &logEvent{event: nil}
	}
	return &logEvent{event: e}
}

// newTrackedLogEvent creates a new tracked LogEvent that decrements activeOps when finished
// (on Msg/Msgf/Send calls).
func newTrackedLogEvent(e *zerolog.Event, s *Service, location string) LogEvent {
	if e == nil || s == nil {
		// If event is nil, we need to decrement the counter that was already incremented
		// by the caller (logEventBuilder or newTrackedContextLogEvent)
		if s != nil {
			s.activeOps.Add(-1)
			s.wg.Done()
			if location != "" {
				s.mu.Lock()
				if s.activeOpLocations != nil {
					s.activeOpLocations[location]--
					if s.activeOpLocations[location] <= 0 {
						delete(s.activeOpLocations, location)
					}
				}
				s.mu.Unlock()
			}
		}
		return &logEvent{event: nil}
	}
	return &trackedLogEvent{
		logEvent: logEvent{event: e},
		service:  s,
		location: location,
	}
}

// newTrackedContextLogEvent creates a tracked log event for context loggers
// that share the same underlying Service lifecycle.
func newTrackedContextLogEvent(cl *contextLogger, level zerolog.Level) LogEvent {
	if cl == nil || cl.logger == nil || cl.parent == nil {
		return newLogEvent(nil)
	}

	// Acquire read lock to prevent Close() from running
	cl.parent.mu.RLock()
	defer cl.parent.mu.RUnlock()

	if !cl.parent.isInitialized.Load() {
		return newLogEvent(nil)
	}

	if cl.logger.GetLevel() > level {
		return newLogEvent(nil)
	}

	// Increment active operations counter ONLY if a log event will be created
	cl.parent.activeOps.Add(1)
	cl.parent.wg.Add(1)

	var event *zerolog.Event
	switch level {
	case zerolog.DebugLevel:
		event = cl.logger.Debug()
	case zerolog.InfoLevel:
		event = cl.logger.Info()
	case zerolog.WarnLevel:
		event = cl.logger.Warn()
	case zerolog.ErrorLevel:
		event = cl.logger.Error()
	case zerolog.FatalLevel:
		event = cl.logger.Fatal()
	case zerolog.PanicLevel:
		event = cl.logger.Panic()
	case zerolog.TraceLevel:
		event = cl.logger.Trace()
	default:
		// Should not happen, but decrement counter if it does
		cl.parent.activeOps.Add(-1)
		cl.parent.wg.Done()
		return newLogEvent(nil)
	}

	return newTrackedLogEvent(event, cl.parent, "")
}

func (e *logEvent) Str(key, val string) LogEvent {
	if e.event != nil {
		e.event.Str(key, val)
	}
	return e
}

func (e *logEvent) Strs(key string, vals []string) LogEvent {
	if e.event != nil {
		e.event.Strs(key, vals)
	}
	return e
}

func (e *logEvent) Stringer(key string, val interface{ String() string }) LogEvent {
	if e.event != nil {
		e.event.Stringer(key, val)
	}
	return e
}

func (e *logEvent) Int(key string, val int) LogEvent {
	if e.event != nil {
		e.event.Int(key, val)
	}
	return e
}

func (e *logEvent) Int8(key string, val int8) LogEvent {
	if e.event != nil {
		e.event.Int8(key, val)
	}
	return e
}

func (e *logEvent) Int16(key string, val int16) LogEvent {
	if e.event != nil {
		e.event.Int16(key, val)
	}
	return e
}

func (e *logEvent) Int32(key string, val int32) LogEvent {
	if e.event != nil {
		e.event.Int32(key, val)
	}
	return e
}

func (e *logEvent) Int64(key string, val int64) LogEvent {
	if e.event != nil {
		e.event.Int64(key, val)
	}
	return e
}

func (e *logEvent) Uint(key string, val uint) LogEvent {
	if e.event != nil {
		e.event.Uint(key, val)
	}
	return e
}

func (e *logEvent) Uint8(key string, val uint8) LogEvent {
	if e.event != nil {
		e.event.Uint8(key, val)
	}
	return e
}

func (e *logEvent) Uint16(key string, val uint16) LogEvent {
	if e.event != nil {
		e.event.Uint16(key, val)
	}
	return e
}

func (e *logEvent) Uint32(key string, val uint32) LogEvent {
	if e.event != nil {
		e.event.Uint32(key, val)
	}
	return e
}

func (e *logEvent) Uint64(key string, val uint64) LogEvent {
	if e.event != nil {
		e.event.Uint64(key, val)
	}
	return e
}

func (e *logEvent) Float32(key string, val float32) LogEvent {
	if e.event != nil {
		e.event.Float32(key, val)
	}
	return e
}

func (e *logEvent) Float64(key string, val float64) LogEvent {
	if e.event != nil {
		e.event.Float64(key, val)
	}
	return e
}

func (e *logEvent) Bool(key string, val bool) LogEvent {
	if e.event != nil {
		e.event.Bool(key, val)
	}
	return e
}

func (e *logEvent) Bools(key string, vals []bool) LogEvent {
	if e.event != nil {
		e.event.Bools(key, vals)
	}
	return e
}

func (e *logEvent) Time(key string, val time.Time) LogEvent {
	if e.event != nil {
		e.event.Time(key, val)
	}
	return e
}

func (e *logEvent) Dur(key string, val time.Duration) LogEvent {
	if e.event != nil {
		e.event.Dur(key, val)
	}
	return e
}

func (e *logEvent) Err(err error) LogEvent {
	if e.event != nil {
		e.event.Err(err)
		if err != nil {
			chain, ops, root, rootOp := buildErrorChain(err)
			if len(chain) > 0 {
				// include array and joined string for readability
				e.event.Strs("error_chain", chain)
				e.event.Str("error_root", root)
				e.event.Str("error_history", joinChain(chain))
				// include ops if any present
				e.event.Strs("error_ops", ops)
				if rootOp != "" {
					e.event.Str("error_root_op", rootOp)
				}
			}
		}
	}
	return e
}

func (e *logEvent) AnErr(key string, err error) LogEvent {
	if e.event != nil {
		e.event.AnErr(key, err)
		if err != nil {
			chain, ops, root, rootOp := buildErrorChain(err)
			if len(chain) > 0 {
				e.event.Strs(key+"_chain", chain)
				e.event.Str(key+"_root", root)
				e.event.Str(key+"_history", joinChain(chain))
				e.event.Strs(key+"_ops", ops)
				if rootOp != "" {
					e.event.Str(key+"_root_op", rootOp)
				}
			}
		}
	}
	return e
}

func (e *logEvent) Bytes(key string, val []byte) LogEvent {
	if e.event != nil {
		e.event.Bytes(key, val)
	}
	return e
}

func (e *logEvent) Hex(key string, val []byte) LogEvent {
	if e.event != nil {
		e.event.Hex(key, val)
	}
	return e
}

func (e *logEvent) IPAddr(key string, val net.IP) LogEvent {
	if e.event != nil {
		e.event.IPAddr(key, val)
	}
	return e
}

func (e *logEvent) MACAddr(key string, val net.HardwareAddr) LogEvent {
	if e.event != nil {
		e.event.MACAddr(key, val)
	}
	return e
}

func (e *logEvent) Interface(key string, val interface{}) LogEvent {
	if e.event != nil {
		e.event.Interface(key, val)
	}
	return e
}

// Dict for nested objects
func (e *logEvent) Dict(key string, dict func(LogEvent)) LogEvent {
	if e.event != nil {
		dictEvent := zerolog.Dict()
		dict(newLogEvent(dictEvent))
		e.event.Dict(key, dictEvent)
	}
	return e
}

func (e *logEvent) Msg(msg string) {
	if e.event != nil {
		e.event.Msg(msg)
	}
}

func (e *logEvent) Msgf(format string, v ...interface{}) {
	if e.event != nil {
		e.event.Msgf(format, v...)
	}
}

func (e *logEvent) Send() {
	if e.event != nil {
		e.event.Send()
	}
}

// Override Msg, Msgf, and Send for trackedLogEvent to decrement counter
func (e *trackedLogEvent) Msg(msg string) {
	defer func() {
		e.service.activeOps.Add(-1)
		e.service.wg.Done()
		// Also decrement location counter if tracking is enabled
		if e.location != "" {
			e.service.mu.Lock()
			if e.service.activeOpLocations != nil {
				e.service.activeOpLocations[e.location]--
				if e.service.activeOpLocations[e.location] <= 0 {
					delete(e.service.activeOpLocations, e.location)
				}
			}
			e.service.mu.Unlock()
		}
	}()
	if e.event != nil {
		e.event.Msg(msg)
	}
}

func (e *trackedLogEvent) Msgf(format string, v ...interface{}) {
	defer func() {
		e.service.activeOps.Add(-1)
		e.service.wg.Done()
		// Also decrement location counter if tracking is enabled
		if e.location != "" {
			e.service.mu.Lock()
			if e.service.activeOpLocations != nil {
				e.service.activeOpLocations[e.location]--
				if e.service.activeOpLocations[e.location] <= 0 {
					delete(e.service.activeOpLocations, e.location)
				}
			}
			e.service.mu.Unlock()
		}
	}()
	if e.event != nil {
		e.event.Msgf(format, v...)
	}
}

func (e *trackedLogEvent) Send() {
	defer func() {
		e.service.activeOps.Add(-1)
		e.service.wg.Done()
		// Also decrement location counter if tracking is enabled
		if e.location != "" {
			e.service.mu.Lock()
			if e.service.activeOpLocations != nil {
				e.service.activeOpLocations[e.location]--
				if e.service.activeOpLocations[e.location] <= 0 {
					delete(e.service.activeOpLocations, e.location)
				}
			}
			e.service.mu.Unlock()
		}
	}()
	if e.event != nil {
		e.event.Send()
	}
}

// logContext implements LogContext by wrapping zerolog.Context
type logContext struct {
	context zerolog.Context
	service *Service
}

// contextLogger wraps a zerolog.Logger created from a context
// It delegates to the parent Service for resource management to avoid
// race conditions from sharing fileWriter between multiple Service instances
type contextLogger struct {
	logger *zerolog.Logger
	parent *Service
}

func (cl *contextLogger) TraceWith() LogEvent {
	if cl.logger == nil || cl.parent == nil || !cl.parent.isInitialized.Load() {
		return newLogEvent(nil)
	}
	return newTrackedContextLogEvent(cl, zerolog.TraceLevel)
}

func (cl *contextLogger) DebugWith() LogEvent {
	if cl.logger == nil || cl.parent == nil || !cl.parent.isInitialized.Load() {
		return newLogEvent(nil)
	}
	return newTrackedContextLogEvent(cl, zerolog.DebugLevel)
}

func (cl *contextLogger) InfoWith() LogEvent {
	if cl.logger == nil || cl.parent == nil || !cl.parent.isInitialized.Load() {
		return newLogEvent(nil)
	}
	return newTrackedContextLogEvent(cl, zerolog.InfoLevel)
}

func (cl *contextLogger) WarnWith() LogEvent {
	if cl.logger == nil || cl.parent == nil || !cl.parent.isInitialized.Load() {
		return newLogEvent(nil)
	}
	return newTrackedContextLogEvent(cl, zerolog.WarnLevel)
}

func (cl *contextLogger) ErrorWith() LogEvent {
	if cl.logger == nil || cl.parent == nil || !cl.parent.isInitialized.Load() {
		return newLogEvent(nil)
	}
	return newTrackedContextLogEvent(cl, zerolog.ErrorLevel)
}

func (cl *contextLogger) FatalWith() LogEvent {
	if cl.logger == nil || cl.parent == nil || !cl.parent.isInitialized.Load() {
		return newLogEvent(nil)
	}
	return newTrackedContextLogEvent(cl, zerolog.FatalLevel)
}

func (cl *contextLogger) PanicWith() LogEvent {
	if cl.logger == nil || cl.parent == nil || !cl.parent.isInitialized.Load() {
		return newLogEvent(nil)
	}
	return newTrackedContextLogEvent(cl, zerolog.PanicLevel)
}

func (cl *contextLogger) With() LogContext {
	if cl.logger == nil || cl.parent == nil || !cl.parent.isInitialized.Load() {
		return &noopLogContext{}
	}

	// Acquire read lock to prevent Close() from running
	cl.parent.mu.RLock()
	defer cl.parent.mu.RUnlock()

	// Double-check after acquiring lock
	if !cl.parent.isInitialized.Load() {
		return &noopLogContext{}
	}

	return &logContext{
		context: cl.logger.With(),
		service: cl.parent,
	}
}

func (c *logContext) Str(key, val string) LogContext {
	c.context = c.context.Str(key, val)
	return c
}

func (c *logContext) Strs(key string, vals []string) LogContext {
	c.context = c.context.Strs(key, vals)
	return c
}

func (c *logContext) Int(key string, val int) LogContext {
	c.context = c.context.Int(key, val)
	return c
}

func (c *logContext) Int64(key string, val int64) LogContext {
	c.context = c.context.Int64(key, val)
	return c
}

func (c *logContext) Uint(key string, val uint) LogContext {
	c.context = c.context.Uint(key, val)
	return c
}

func (c *logContext) Uint64(key string, val uint64) LogContext {
	c.context = c.context.Uint64(key, val)
	return c
}

func (c *logContext) Float64(key string, val float64) LogContext {
	c.context = c.context.Float64(key, val)
	return c
}

func (c *logContext) Bool(key string, val bool) LogContext {
	c.context = c.context.Bool(key, val)
	return c
}

func (c *logContext) Time(key string, val time.Time) LogContext {
	c.context = c.context.Time(key, val)
	return c
}

func (c *logContext) Err(err error) LogContext {
	c.context = c.context.Err(err)
	return c
}

func (c *logContext) Interface(key string, val interface{}) LogContext {
	c.context = c.context.Interface(key, val)
	return c
}

func (c *logContext) Logger() Logger {
	logger := c.context.Logger()
	// Create a wrapper that delegates to the parent service for resource management
	// This avoids the race condition of sharing fileWriter between multiple Service instances
	newService := &contextLogger{
		logger: &logger,
		parent: c.service,
	}
	return newService
}

// noopLogContext is a no-op implementation of LogContext
type noopLogContext struct{}

func (n *noopLogContext) Str(key, val string) LogContext             { return n }
func (n *noopLogContext) Strs(key string, vals []string) LogContext  { return n }
func (n *noopLogContext) Int(key string, val int) LogContext         { return n }
func (n *noopLogContext) Int64(key string, val int64) LogContext     { return n }
func (n *noopLogContext) Uint(key string, val uint) LogContext       { return n }
func (n *noopLogContext) Uint64(key string, val uint64) LogContext   { return n }
func (n *noopLogContext) Float64(key string, val float64) LogContext { return n }
func (n *noopLogContext) Bool(key string, val bool) LogContext       { return n }
func (n *noopLogContext) Time(key string, val time.Time) LogContext  { return n }
func (n *noopLogContext) Err(err error) LogContext                   { return n }
func (n *noopLogContext) Interface(key string, val interface{}) LogContext {
	return n
}
func (n *noopLogContext) Logger() Logger { return &noopLogger{} }

// noopLogger is a no-op implementation of Logger
type noopLogger struct{}

func (n *noopLogger) TraceWith() LogEvent { return newLogEvent(nil) }
func (n *noopLogger) DebugWith() LogEvent { return newLogEvent(nil) }
func (n *noopLogger) InfoWith() LogEvent  { return newLogEvent(nil) }
func (n *noopLogger) WarnWith() LogEvent  { return newLogEvent(nil) }
func (n *noopLogger) ErrorWith() LogEvent { return newLogEvent(nil) }
func (n *noopLogger) FatalWith() LogEvent { return newLogEvent(nil) }
func (n *noopLogger) PanicWith() LogEvent { return newLogEvent(nil) }
func (n *noopLogger) With() LogContext    { return &noopLogContext{} }
