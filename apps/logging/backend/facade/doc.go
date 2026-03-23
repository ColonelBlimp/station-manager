// Copyright 2026 Station-Manager. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

/*
Package facade provides the backend service layer for the Station Manager logging application.

# Overview

The facade package implements the Facade design pattern, providing a unified interface
between the Wails frontend (JavaScript/TypeScript) and the various backend services.
It acts as the single entry point for all frontend operations, coordinating multiple
subsystems while hiding their complexity from the UI layer.

# Architecture

The Service struct is the central component, registered with Wails for frontend binding.
It orchestrates the following subsystems:

  - ConfigService: Application configuration management
  - LoggerService: Structured logging
  - DatabaseService: SQLite database operations (QSOs, logbooks, contacts)
  - CatService: Radio CAT (Computer Aided Transceiver) control
  - HamnutLookupService: Callsign lookup via HamQTH
  - QrzLookupService: Callsign lookup via QRZ.com
  - EmailService: ADIF file forwarding via email
  - Forwarders: QSO upload to online services (QRZ.com logbook, etc.)

# Lifecycle

The facade follows a strict lifecycle:

 1. Initialize() - Validates dependencies and sets up internal state (called once via sync.Once)
 2. SetContainer() - Injects the IOC container for dynamic service resolution
 3. Start(ctx) - Opens database, starts CAT service, launches worker goroutines
 4. Stop() - Gracefully shuts down workers, closes database, cleans up resources

# Concurrency Model

The facade manages several concurrent subsystems:

  - CAT Status Listener: Receives radio status updates and emits events to the frontend
  - QSO Forwarding Workers: Pool of workers that upload QSOs to online services
  - DB Write Worker: Serializes all database writes to prevent SQLite busy errors
  - Polling Loop: Periodically checks for pending QSO uploads

All workers respond to context cancellation and shutdown signals for graceful termination.

# Frontend Integration

Methods on the Service struct are automatically bound to the frontend via Wails.
The frontend can call these methods directly:

  - FetchUiConfig() - Get UI configuration
  - NewQso(callsign) - Initialize a new QSO with callsign lookup
  - LogQso(qso) - Save a QSO to the database
  - UpdateQso(qso) - Update an existing QSO
  - Ready() - Signal that the UI is ready to receive CAT updates

Events are emitted to the frontend using Wails runtime.EventsEmit for real-time updates
(e.g., radio frequency/mode changes).

# Validation

QSO data is validated using go-playground/validator with custom validators for:

  - Amateur radio bands (e.g., "20m", "40m")
  - Operating modes (e.g., "SSB", "CW")
  - ADIF date format (YYYYMMDD)
  - ADIF time format (HHMM or HHMMSS)
  - RST signal reports
  - Frequency in MHz

# Error Handling

The package uses the custom errors package for operation tracing. Each function
defines an errors.Op constant for the call chain. Errors returned to the frontend
use errors.Root() to provide clean error messages without internal implementation details.

# Example Usage

The facade is typically instantiated via dependency injection:

	container := iocdi.New()
	container.Register(facade.ServiceName, reflect.TypeOf((*facade.Service)(nil)))
	// ... register other services ...
	container.Build()

	svc, _ := container.ResolveSafe(facade.ServiceName)
	facade := svc.(*facade.Service)
	facade.SetContainer(container)
	facade.Start(ctx)
*/
package facade
