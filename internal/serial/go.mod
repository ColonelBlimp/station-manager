module github.com/ColonelBlimp/station-manager/internal/serial

go 1.25.0

require (
	github.com/Station-Manager/errors v0.0.11
	github.com/Station-Manager/types v0.0.64
	go.bug.st/serial v1.6.4
)

require (
	github.com/creack/goselect v0.1.3 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/ColonelBlimp/station-manager/internal/config => ../config

replace github.com/ColonelBlimp/station-manager/internal/enums => ../enums

replace github.com/ColonelBlimp/station-manager/internal/errors => ../errors

replace github.com/ColonelBlimp/station-manager/internal/logging => ../logging

replace github.com/ColonelBlimp/station-manager/internal/types => ../types

replace github.com/ColonelBlimp/station-manager/internal/utils => ../utils
