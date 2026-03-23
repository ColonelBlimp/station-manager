package config

import (
	"github.com/Station-Manager/enums/cmds"
	"github.com/Station-Manager/enums/tags"
	"github.com/Station-Manager/types"
)

var postgresConfig = types.DatastoreConfig{
	Driver:                    types.PostgresDriverName,
	Host:                      "localhost",
	Port:                      5432,
	Database:                  "station_manager",
	User:                      "smuser",
	Password:                  "",
	SSLMode:                   "disable",
	Debug:                     false,
	MaxOpenConns:              10, // General-purpose default; tune per deployment
	MaxIdleConns:              5,
	ConnMaxLifetime:           30, // Minutes
	ConnMaxIdleTime:           5,  // Minutes
	ContextTimeout:            5,  // Seconds
	TransactionContextTimeout: 15, // Seconds
}

var sqliteConfig = types.DatastoreConfig{
	Driver:                    types.SqliteDriverName,
	Path:                      "db/data.db",
	Options:                   map[string]string{"mode": "rwc", "_foreign_keys": "on", "_journal_mode": "WAL", "_busy_timeout": "10000"},
	Debug:                     false,
	MaxOpenConns:              4, // Readers benefit; writes still serialized
	MaxIdleConns:              4,
	ConnMaxLifetime:           0,  // No forced recycle for file-backed DB
	ConnMaxIdleTime:           5,  // Minutes
	ContextTimeout:            15, // Seconds
	TransactionContextTimeout: 20, // Seconds
}

var defaultDesktopConfig = types.AppConfig{
	LoggingConfig: types.LoggingConfig{
		Level:                  "info",
		WithTimestamp:          true,
		ConsoleLogging:         false,
		FileLogging:            true,
		RelLogFileDir:          "logs",
		SkipFrameCount:         3,
		LogFileMaxSizeMB:       100,
		LogFileMaxAgeDays:      30,
		LogFileMaxBackups:      5,
		ShutdownTimeoutMS:      10000,
		ShutdownTimeoutWarning: false,
	},
	DatastoreConfig:      sqliteConfig,
	RequiredConfigs:      defaultRequiredConfigs,
	RigConfigs:           defaultRigConfigs,
	LookupServiceConfigs: defaultLookupServiceConfigs,
	LoggingStation:       defaultLoggingStationDetails,
	EmailConfigs:         defaultEmailConfigs,
	ForwardingConfigs:    defaultForwardingConfigs,
	OptionalConfigs:      defaultOptionalConfigs,
	ListenerConfigs:      defaultListenerConfigs,
}

var defaultServerConfig = types.AppConfig{
	DatastoreConfig: postgresConfig,
	LoggingConfig: types.LoggingConfig{
		Level:                  "info",
		WithTimestamp:          true,
		ConsoleLogging:         false,
		FileLogging:            true,
		RelLogFileDir:          "logs",
		SkipFrameCount:         3,
		LogFileMaxSizeMB:       100,
		LogFileMaxAgeDays:      30,
		LogFileMaxBackups:      5,
		ShutdownTimeoutMS:      10000,
		ShutdownTimeoutWarning: false,
	},
	RequiredConfigs: types.RequiredConfigs{},
	ServerConfig: &types.ServerConfig{
		Name:         "Station Manager",
		Port:         3000,
		ReadTimeout:  5,       // Seconds
		WriteTimeout: 10,      // Seconds
		IdleTimeout:  60,      // Seconds
		BodyLimit:    2097152, // 1024 * 1024 * 2 = 2MB
	},
}

var defaultRequiredConfigs = types.RequiredConfigs{
	SetupComplete:      false, // Indicates whether setup has been completed
	DefaultLogbookID:   1,
	DefaultRigID:       1,
	DefaultFreq:        "14.300.000",
	DefaultMode:        "USB",
	DefaultIsRandomQso: true,
	DefaultTxPower:     50,
	PowerMultiplier:    10, // 1 equals no power multiplier
	UsePowerMultiplier: true,
	DefaultFwdEmail:    "",

	/*
		General configs to do with forwarding QSOs to online services.
	*/
	QsoForwardingPollIntervalSeconds: 120, // Poll every 120 seconds
	QsoForwardingWorkerCount:         5,
	QsoForwardingQueueSize:           20,
	QsoForwardingRowLimit:            5,
	DatabaseWriteQueueSize:           100,

	PagingationPageSize: 24,
}

var defaultLookupServiceConfigs = []types.LookupConfig{
	{
		Name:           types.HamNutLookupServiceName,
		URL:            "https://api.hamnut.com/v1/call-signs/prefixes",
		Enabled:        false,
		HttpTimeoutSec: 5, // Seconds
		UserAgent:      userAgent,
	},
	{
		Name:           types.QrzLookupServiceName,
		URL:            "https://xmldata.qrz.com/xml/current/",
		Username:       "?",
		Password:       "?",
		Enabled:        false,
		HttpTimeoutSec: 5, // Seconds
		UserAgent:      userAgent,
	},
}

var defaultLoggingStationDetails = types.LoggingStation{
	AntennaAzimuth:  "",
	MyAltitude:      "",
	MyAntenna:       "",
	MyCity:          "",
	MyCountry:       "",
	MyCqZone:        "",
	MyDXCC:          "",
	MyGridsquare:    "",
	MyIota:          "",
	MyIotaIslandID:  "",
	MyITUZone:       "",
	MyLat:           "",
	MyLon:           "",
	MyMorseKeyInfo:  "",
	MyMorseKeyType:  "",
	MyName:          "",
	MyPostalCode:    "",
	MyRig:           "",
	MyStreet:        "",
	MyWwffRef:       "",
	Operator:        "",
	OwnerCallsign:   "",
	StationCallsign: "",
}

var defaultEmailConfigs = types.EmailConfig{
	Name:               types.EmailServiceName,
	Enabled:            false,
	Username:           "?",
	Password:           "?",
	Host:               "?",
	Port:               587,
	From:               "?",
	To:                 "?",
	Subject:            "",
	Body:               "",
	SmtpDialTimeoutSec: 10,
	SmtpRetryCount:     0,
	SmtpRetryDelaySec:  0,
}

var defaultForwardingConfigs = []types.ForwarderConfig{
	{
		Name:           types.QrzForwardingServiceName,
		Enabled:        false,
		URL:            "",
		APIKey:         "",
		Username:       "",
		Password:       "",
		UserAgent:      userAgent,
		HttpTimeoutSec: 5, // Seconds
	},
}

var defaultOptionalConfigs = types.OptionalConfigs{
	QrzViewUrl: "https://www.qrz.com/db/", // This MUST end with a slash!
}

var defaultRigConfigs = []types.RigConfig{
	{
		ID:    1,
		Name:  "FTdx10",
		Model: "Yaesu FTdx10",
		SerialConfig: types.SerialConfig{
			PortName:       "/dev/ttyUSB0",
			BaudRate:       38400,
			DataBits:       8,
			Parity:         0,
			StopBits:       0,
			ReadTimeoutMS:  8, // Per-read timeout on the serial driver; size this <= ListenerRateLimiterIntervalMS
			WriteTimeoutMS: 20,
			RTS:            true,
			DTR:            true,
			LineDelimiter:  ';',
		},
		CatCommands: []types.CatCommand{
			{
				Name: cmds.Init.String(),
				Cmd:  "AI1;ID;",
			},
			{
				Name: cmds.Read.String(),
				Cmd:  "FA;FB;ST;VS;MD0;MD1;PC;",
			},
			{
				Name: cmds.PlayBack.String(),
				Cmd:  "PB0%s;",
			},
		},
		CatStates: []types.CatState{
			{
				Prefix: "ID",
				Markers: []types.Marker{
					{
						Tag:    tags.Identity.String(),
						Index:  0,
						Length: 4,
						ValueMappings: []types.ValueMapping{
							{
								Key:   "0761",
								Value: "FTdx10",
							},
						},
					},
				},
			},
			{
				Prefix: "FA",
				Markers: []types.Marker{
					{
						Tag:    tags.VfoAFreq.String(),
						Index:  0,
						Length: 9,
					},
				},
			},
			{
				Prefix: "FB",
				Markers: []types.Marker{
					{
						Tag:    tags.VfoBFreq.String(),
						Index:  0,
						Length: 9,
					},
				},
			},
			{
				Prefix: "ST",
				Markers: []types.Marker{
					{
						Tag:    tags.Split.String(),
						Index:  0,
						Length: 1,
						ValueMappings: []types.ValueMapping{
							{
								Key:   "0",
								Value: "OFF",
							},
							{
								Key:   "1",
								Value: "ON",
							},
							{
								Key:   "2",
								Value: "ON+",
							},
						},
					},
				},
			},
			{
				Prefix: "VS",
				Markers: []types.Marker{
					{
						Tag:    tags.Select.String(),
						Index:  0,
						Length: 1,
						ValueMappings: []types.ValueMapping{
							{
								Key:   "0",
								Value: "VFO-A",
							},
							{
								Key:   "1",
								Value: "VFO-B",
							},
						},
					},
				},
			},
			{
				Prefix: "MD0",
				Markers: []types.Marker{
					{
						Tag:    tags.MainMode.String(),
						Index:  0,
						Length: 1,
						ValueMappings: []types.ValueMapping{
							{
								Key:   "1",
								Value: "LSB",
							},
							{
								Key:   "2",
								Value: "USB",
							},
							{
								Key:   "3",
								Value: "CW-U",
							},
							{
								Key:   "4",
								Value: "FM",
							},
							{
								Key:   "5",
								Value: "AM",
							},
							{
								Key:   "6",
								Value: "RTTY-L",
							},
							{
								Key:   "7",
								Value: "CW-L",
							},
							{
								Key:   "8",
								Value: "DATA-L",
							},
							{
								Key:   "9",
								Value: "RTTY-U",
							},
							{
								Key:   "A",
								Value: "DATA-FM",
							},
							{
								Key:   "B",
								Value: "FM-N",
							},
							{
								Key:   "C",
								Value: "DATA-U",
							},
							{
								Key:   "D",
								Value: "AM-N",
							},
							{
								Key:   "E",
								Value: "PSK",
							},
							{
								Key:   "F",
								Value: "DATA-FM-N",
							},
						},
					},
				},
			},
			{
				Prefix: "MD1",
				Markers: []types.Marker{
					{
						Tag:    tags.SubMode.String(),
						Index:  0,
						Length: 1,
						ValueMappings: []types.ValueMapping{
							{
								Key:   "1",
								Value: "LSB",
							},
							{
								Key:   "2",
								Value: "USB",
							},
							{
								Key:   "3",
								Value: "CW-U",
							},
							{
								Key:   "4",
								Value: "FM",
							},
							{
								Key:   "5",
								Value: "AM",
							},
							{
								Key:   "6",
								Value: "RTTY-L",
							},
							{
								Key:   "7",
								Value: "CW-L",
							},
							{
								Key:   "8",
								Value: "DATA-L",
							},
							{
								Key:   "9",
								Value: "RTTY-U",
							},
							{
								Key:   "A",
								Value: "DATA-FM",
							},
							{
								Key:   "B",
								Value: "FM-N",
							},
							{
								Key:   "C",
								Value: "DATA-U",
							},
							{
								Key:   "D",
								Value: "AM-N",
							},
							{
								Key:   "E",
								Value: "PSK",
							},
							{
								Key:   "F",
								Value: "DATA-FM-N",
							},
						},
					},
				},
			},
			{
				Prefix: "PC",
				Markers: []types.Marker{
					{
						Tag:    tags.TxPwr.String(),
						Index:  0,
						Length: 3,
					},
				},
			},
		},
		CatConfig: types.CatConfig{
			Enabled: false,
			// ListenerRateLimiterInterval controls how often the CAT listener polls the serial port.
			// It should be greater than or equal to ListenerReadTimeoutMS, so each tick's read
			// can complete or time out before the next tick fires.
			ListenerRateLimiterIntervalMS: 10,
			// ListenerReadTimeoutMS is the per-tick timeout used by the CAT listener when
			// waiting for a framed response from the serial client. This is typically sized
			// to be <= ListenerRateLimiterInterval and may match SerialConfig.ReadTimeoutMS.
			ListenerReadTimeoutMS: 8,

			SendChannelSize:       10,
			ProcessingChannelSize: 10,
		},
	},
}

var defaultListenerConfigs = []types.ListenerConfig{
	{
		Name:       "WSJT-X",
		Enabled:    true,
		Host:       "localhost",
		Port:       2237,
		Protocol:   "UDP",
		BufferSize: 1024,
		LogPayload: false,
		Handler:    "wsjtx",
		HandlerConfig: map[string]any{
			"auto_log":      false,
			"log_decodes":   false,
			"message_types": []int{12},
		},
	},
}
