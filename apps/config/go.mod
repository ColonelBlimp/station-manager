module github.com/ColonelBlimp/station-manager/apps/config

go 1.25.0

require github.com/ColonelBlimp/station-manager/internal v0.0.0

require (
	github.com/goccy/go-json v0.10.6 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/text v0.35.0 // indirect
)

replace github.com/ColonelBlimp/station-manager/internal => ../../internal
