module github.com/ColonelBlimp/station-manager/internal/adapters

go 1.25.0

require (
	github.com/Station-Manager/adapters v0.0.11
	github.com/Station-Manager/database v0.0.39
	github.com/Station-Manager/errors v0.0.11
	github.com/Station-Manager/types v0.0.64
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
	github.com/lib/pq v1.11.2 // indirect
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
