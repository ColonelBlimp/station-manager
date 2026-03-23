module github.com/ColonelBlimp/station-manager/internal/adapters

go 1.25.0

require (
	github.com/ColonelBlimp/station-manager/internal/database v0.0.0
	github.com/ColonelBlimp/station-manager/internal/errors v0.0.0
	github.com/ColonelBlimp/station-manager/internal/types v0.0.0
	github.com/aarondl/null/v8 v8.1.3
	github.com/aarondl/sqlboiler/v4 v4.19.7
	github.com/goccy/go-json v0.10.6
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/aarondl/inflect v0.0.2 // indirect
	github.com/aarondl/randomize v0.0.2 // indirect
	github.com/aarondl/strmangle v0.0.9 // indirect
	github.com/creack/goselect v0.1.3 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/ericlagergren/decimal v0.0.0-20240411145413-00de7ca16731 // indirect
	github.com/friendsofgo/errors v0.9.2 // indirect
	github.com/gofrs/uuid v4.4.0+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/lib/pq v1.12.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	go.bug.st/serial v1.6.4 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/ColonelBlimp/station-manager/internal/database => ../database

replace github.com/ColonelBlimp/station-manager/internal/errors => ../errors

replace github.com/ColonelBlimp/station-manager/internal/types => ../types

replace github.com/ColonelBlimp/station-manager/internal/adif => ../adif

replace github.com/ColonelBlimp/station-manager/internal/apikey => ../apikey

replace github.com/ColonelBlimp/station-manager/internal/cat => ../cat

replace github.com/ColonelBlimp/station-manager/internal/config => ../config

replace github.com/ColonelBlimp/station-manager/internal/email => ../email

replace github.com/ColonelBlimp/station-manager/internal/enums => ../enums

replace github.com/ColonelBlimp/station-manager/internal/forwarding => ../forwarding

replace github.com/ColonelBlimp/station-manager/internal/iocdi => ../iocdi

replace github.com/ColonelBlimp/station-manager/internal/listeners => ../listeners

replace github.com/ColonelBlimp/station-manager/internal/logging => ../logging

replace github.com/ColonelBlimp/station-manager/internal/lookup => ../lookup

replace github.com/ColonelBlimp/station-manager/internal/maidenhead => ../maidenhead

replace github.com/ColonelBlimp/station-manager/internal/serial => ../serial

replace github.com/ColonelBlimp/station-manager/internal/utils => ../utils
