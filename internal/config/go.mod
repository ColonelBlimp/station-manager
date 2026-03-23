module github.com/ColonelBlimp/station-manager/internal/config

go 1.25.0

require (
	github.com/ColonelBlimp/station-manager/pkg/enums v0.0.0
	github.com/ColonelBlimp/station-manager/pkg/errors v0.0.0
	github.com/ColonelBlimp/station-manager/pkg/types v0.0.0
	github.com/ColonelBlimp/station-manager/pkg/utils v0.0.0
	github.com/goccy/go-json v0.10.6
)

require (
	github.com/creack/goselect v0.1.3 // indirect
	go.bug.st/serial v1.6.4 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
)

replace github.com/ColonelBlimp/station-manager/pkg/enums => ../../pkg/enums

replace github.com/ColonelBlimp/station-manager/pkg/errors => ../../pkg/errors

replace github.com/ColonelBlimp/station-manager/pkg/types => ../../pkg/types

replace github.com/ColonelBlimp/station-manager/pkg/utils => ../../pkg/utils
