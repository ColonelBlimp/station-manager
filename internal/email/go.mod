module github.com/ColonelBlimp/station-manager/internal/email

go 1.25.0

require (
	github.com/ColonelBlimp/station-manager/internal/adif v0.0.0-00010101000000-000000000000
	github.com/ColonelBlimp/station-manager/internal/config v0.0.0
	github.com/ColonelBlimp/station-manager/internal/errors v0.0.0
	github.com/ColonelBlimp/station-manager/internal/logging v0.0.0
	github.com/ColonelBlimp/station-manager/internal/types v0.0.0
)

require (
	github.com/ColonelBlimp/station-manager/internal/enums v0.0.0 // indirect
	github.com/ColonelBlimp/station-manager/internal/utils v0.0.0 // indirect
	github.com/creack/goselect v0.1.3 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.1 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	go.bug.st/serial v1.6.4 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)

replace github.com/ColonelBlimp/station-manager/internal/config => ../config

replace github.com/ColonelBlimp/station-manager/internal/errors => ../errors

replace github.com/ColonelBlimp/station-manager/internal/iocdi => ../iocdi

replace github.com/ColonelBlimp/station-manager/internal/logging => ../logging

replace github.com/ColonelBlimp/station-manager/internal/types => ../types

replace github.com/ColonelBlimp/station-manager/internal/enums => ../enums

replace github.com/ColonelBlimp/station-manager/internal/utils => ../utils

replace github.com/ColonelBlimp/station-manager/internal/adif => ../adif

replace github.com/ColonelBlimp/station-manager/internal/adapters => ../adapters

replace github.com/ColonelBlimp/station-manager/internal/database => ../database

replace github.com/ColonelBlimp/station-manager/internal/apikey => ../apikey

replace github.com/ColonelBlimp/station-manager/internal/cat => ../cat

replace github.com/ColonelBlimp/station-manager/internal/forwarding => ../forwarding

replace github.com/ColonelBlimp/station-manager/internal/listeners => ../listeners

replace github.com/ColonelBlimp/station-manager/internal/lookup => ../lookup

replace github.com/ColonelBlimp/station-manager/internal/maidenhead => ../maidenhead

replace github.com/ColonelBlimp/station-manager/internal/serial => ../serial
