module github.com/ColonelBlimp/station-manager/internal/logging

go 1.25.0

require (
	github.com/ColonelBlimp/station-manager/internal/config v0.0.0
	github.com/ColonelBlimp/station-manager/internal/errors v0.0.0
	github.com/ColonelBlimp/station-manager/internal/types v0.0.0
	github.com/ColonelBlimp/station-manager/internal/utils v0.0.0
	github.com/go-playground/validator/v10 v10.30.1
	github.com/rs/zerolog v1.34.0
	github.com/stretchr/testify v1.11.1
	go.uber.org/atomic v1.11.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require (
	github.com/ColonelBlimp/station-manager/internal/enums v0.0.0 // indirect
	github.com/creack/goselect v0.1.3 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	go.bug.st/serial v1.6.4 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/ColonelBlimp/station-manager/internal/config => ../config

replace github.com/ColonelBlimp/station-manager/internal/enums => ../enums

replace github.com/ColonelBlimp/station-manager/internal/errors => ../errors

replace github.com/ColonelBlimp/station-manager/internal/types => ../types

replace github.com/ColonelBlimp/station-manager/internal/utils => ../utils

replace github.com/ColonelBlimp/station-manager/internal/adapters => ../adapters

replace github.com/ColonelBlimp/station-manager/internal/adif => ../adif

replace github.com/ColonelBlimp/station-manager/internal/apikey => ../apikey

replace github.com/ColonelBlimp/station-manager/internal/cat => ../cat

replace github.com/ColonelBlimp/station-manager/internal/database => ../database

replace github.com/ColonelBlimp/station-manager/internal/email => ../email

replace github.com/ColonelBlimp/station-manager/internal/forwarding => ../forwarding

replace github.com/ColonelBlimp/station-manager/internal/iocdi => ../iocdi

replace github.com/ColonelBlimp/station-manager/internal/listeners => ../listeners

replace github.com/ColonelBlimp/station-manager/internal/lookup => ../lookup

replace github.com/ColonelBlimp/station-manager/internal/maidenhead => ../maidenhead

replace github.com/ColonelBlimp/station-manager/internal/serial => ../serial
