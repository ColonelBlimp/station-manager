module github.com/ColonelBlimp/station-manager/apps/config

go 1.25.0

require (
	github.com/ColonelBlimp/station-manager/internal/errors v0.0.0
	github.com/ColonelBlimp/station-manager/internal/utils v0.0.0
)

require (
	github.com/goccy/go-json v0.10.6 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/text v0.35.0 // indirect
)

replace github.com/ColonelBlimp/station-manager/internal/adapters => ../../internal/adapters

replace github.com/ColonelBlimp/station-manager/internal/adif => ../../internal/adif

replace github.com/ColonelBlimp/station-manager/internal/apikey => ../../internal/apikey

replace github.com/ColonelBlimp/station-manager/internal/cat => ../../internal/cat

replace github.com/ColonelBlimp/station-manager/internal/config => ../../internal/config

replace github.com/ColonelBlimp/station-manager/internal/database => ../../internal/database

replace github.com/ColonelBlimp/station-manager/internal/email => ../../internal/email

replace github.com/ColonelBlimp/station-manager/internal/enums => ../../internal/enums

replace github.com/ColonelBlimp/station-manager/internal/errors => ../../internal/errors

replace github.com/ColonelBlimp/station-manager/internal/forwarding => ../../internal/forwarding

replace github.com/ColonelBlimp/station-manager/internal/iocdi => ../../internal/iocdi

replace github.com/ColonelBlimp/station-manager/internal/listeners => ../../internal/listeners

replace github.com/ColonelBlimp/station-manager/internal/logging => ../../internal/logging

replace github.com/ColonelBlimp/station-manager/internal/lookup => ../../internal/lookup

replace github.com/ColonelBlimp/station-manager/internal/maidenhead => ../../internal/maidenhead

replace github.com/ColonelBlimp/station-manager/internal/serial => ../../internal/serial

replace github.com/ColonelBlimp/station-manager/internal/types => ../../internal/types

replace github.com/ColonelBlimp/station-manager/internal/utils => ../../internal/utils
