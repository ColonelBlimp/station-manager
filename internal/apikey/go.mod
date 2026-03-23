module github.com/ColonelBlimp/station-manager/internal/apikey

go 1.25.0

require golang.org/x/crypto v0.49.0

require golang.org/x/sys v0.42.0 // indirect

replace github.com/ColonelBlimp/station-manager/internal/adapters => ../adapters

replace github.com/ColonelBlimp/station-manager/internal/adif => ../adif

replace github.com/ColonelBlimp/station-manager/internal/cat => ../cat

replace github.com/ColonelBlimp/station-manager/internal/config => ../config

replace github.com/ColonelBlimp/station-manager/internal/database => ../database

replace github.com/ColonelBlimp/station-manager/internal/email => ../email

replace github.com/ColonelBlimp/station-manager/internal/enums => ../enums

replace github.com/ColonelBlimp/station-manager/internal/errors => ../errors

replace github.com/ColonelBlimp/station-manager/internal/forwarding => ../forwarding

replace github.com/ColonelBlimp/station-manager/internal/iocdi => ../iocdi

replace github.com/ColonelBlimp/station-manager/internal/listeners => ../listeners

replace github.com/ColonelBlimp/station-manager/internal/logging => ../logging

replace github.com/ColonelBlimp/station-manager/internal/lookup => ../lookup

replace github.com/ColonelBlimp/station-manager/internal/maidenhead => ../maidenhead

replace github.com/ColonelBlimp/station-manager/internal/serial => ../serial

replace github.com/ColonelBlimp/station-manager/internal/types => ../types

replace github.com/ColonelBlimp/station-manager/internal/utils => ../utils
