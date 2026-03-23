module github.com/ColonelBlimp/station-manager/internal/config

go 1.25.0

require (
	github.com/ColonelBlimp/station-manager/internal/enums v0.0.0
	github.com/ColonelBlimp/station-manager/internal/errors v0.0.0
	github.com/ColonelBlimp/station-manager/internal/types v0.0.0
	github.com/ColonelBlimp/station-manager/internal/utils v0.0.0
	github.com/goccy/go-json v0.10.6
)

require (
	github.com/creack/goselect v0.1.3 // indirect
	go.bug.st/serial v1.6.4 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
)

replace github.com/ColonelBlimp/station-manager/internal/enums => ../enums

replace github.com/ColonelBlimp/station-manager/internal/errors => ../errors

replace github.com/ColonelBlimp/station-manager/internal/types => ../types

replace github.com/ColonelBlimp/station-manager/internal/utils => ../utils
