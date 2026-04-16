// Package iocdi is a lightweight IoC/DI container that wires services
// together using struct tags and reflection.
//
// # Usage
//
// Exported struct fields tagged with `di.inject:"beanID"` declare a
// dependency on the bean registered under that ID. The container resolves
// and injects all tagged fields during [Container.Build]. Bean IDs are
// case-insensitive (normalised to lowercase internally).
//
//	type MyService struct {
//	    Logger *logging.Service `di.inject:"loggingservice"`
//	    Config *config.Service  `di.inject:"configservice"`
//	}
//
// # Lifecycle
//
// Registration ([Container.Register], [Container.RegisterInstance]) is open
// until [Container.Build] is called. Build is idempotent and:
//
//  1. Verifies all required dependencies are registered and type-compatible.
//  2. Instantiates uninstantiated beans (pointer-to-struct allocation).
//  3. Injects dependencies via DFS traversal with cycle detection.
//  4. Calls [Initializer].Initialize() on beans that implement it, in
//     dependency order (a bean's dependencies are initialised before the
//     bean itself).
//
// After Build, [Container.Resolve] / [Container.ResolveSafe] / [ResolveAs]
// return wired singleton instances by bean ID.
//
// # Why home-grown
//
// This container is deliberately simpler than uber-go/fx or google/wire.
// It covers exactly what the project needs (tag-based injection, singleton
// lifecycle, ordered initialisation, cycle detection) without bringing in
// a large dependency or code-generation step. See CLAUDE.md → "Home-grown
// choices are deliberate."
package iocdi
