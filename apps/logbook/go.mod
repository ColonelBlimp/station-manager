module github.com/ColonelBlimp/station-manager/apps/logbook

go 1.25.0

require (
	github.com/ColonelBlimp/station-manager/internal/config v0.0.0
	github.com/ColonelBlimp/station-manager/internal/database v0.0.0
	github.com/ColonelBlimp/station-manager/internal/email v0.0.0
	github.com/ColonelBlimp/station-manager/internal/enums v0.0.0
	github.com/ColonelBlimp/station-manager/internal/errors v0.0.0
	github.com/ColonelBlimp/station-manager/internal/iocdi v0.0.0
	github.com/ColonelBlimp/station-manager/internal/logging v0.0.0
	github.com/ColonelBlimp/station-manager/internal/types v0.0.0
	github.com/ColonelBlimp/station-manager/internal/utils v0.0.0
	github.com/aarondl/sqlboiler/v4 v4.19.7
	github.com/go-playground/validator/v10 v10.30.1
	github.com/wailsapp/wails/v2 v2.11.0
)

require (
	github.com/ColonelBlimp/station-manager/internal/adif v0.0.0 // indirect
	github.com/aarondl/inflect v0.0.2 // indirect
	github.com/aarondl/null/v8 v8.1.3 // indirect
	github.com/aarondl/randomize v0.0.2 // indirect
	github.com/aarondl/strmangle v0.0.9 // indirect
	github.com/bep/debounce v1.2.1 // indirect
	github.com/creack/goselect v0.1.3 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ericlagergren/decimal v0.0.0-20240411145413-00de7ca16731 // indirect
	github.com/friendsofgo/errors v0.9.2 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/gofrs/uuid v4.4.0+incompatible // indirect
	github.com/golang-migrate/migrate/v4 v4.19.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/labstack/echo/v4 v4.15.1 // indirect
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/leaanthony/go-ansi-parser v1.6.1 // indirect
	github.com/leaanthony/gosod v1.0.4 // indirect
	github.com/leaanthony/slicer v1.6.0 // indirect
	github.com/leaanthony/u v1.1.1 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lib/pq v1.12.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-sqlite3 v1.14.37 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/samber/lo v1.53.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/tkrajina/go-reflector v0.5.8 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/wailsapp/go-webview2 v1.0.23 // indirect
	github.com/wailsapp/mimetype v1.4.1 // indirect
	go.bug.st/serial v1.6.4 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	modernc.org/libc v1.70.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.47.0 // indirect
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
