package types

import "time"

type CatCommand struct {
	Name string
	Cmd  string
}

type CatState struct {
	Prefix  string // The CAT command prefix.
	Markers []Marker
	Data    string
}

type Marker struct {
	Tag           string
	Index         int
	Length        int
	ValueMappings []ValueMapping
}

type ValueMapping struct {
	Key   string
	Value string
}

// CatStatus is a map of CAT status values keyed by state tag. These are related to CAT commands; a particular
// command when issued will return a status value linked to that command. The status value is a string that
// represents the status of the CAT command in a format usable by the frontend.
//
// See the cat.StatusChannel in the cat/service.go file for more details.
type CatStatus map[string]string

// StateValues is a map of state values keyed by state tag. These are related to CAT commands; a particular command
// when issued will return a state value linked to that command. The state value is a string that represents the
// state of the CAT command in user-readable format.
//
// See the cat.StateValues() method in the cat/service.go file for more details.
type StateValues map[string]map[string]string

type CatConfig struct {
	Enabled bool
	// ListenerRateLimiterIntervalMS controls how frequently the CAT listener will
	// poll the serial port for new data. The unit is milliseconds.
	//
	// Default is 10ms.
	ListenerRateLimiterIntervalMS time.Duration
	// ListenerReadTimeoutMS controls how long each CAT listener cycle will wait for
	// a framed response line from the serial client. This should typically be less
	// than or equal to ListenerRateLimiterInterval so that each tick's read can
	// complete or time out before the next tick occurs. The unit is milliseconds.
	//
	// If left as zero, callers may choose a sensible default or fall back to the
	// underlying SerialConfig.ReadTimeoutMS.
	//
	// Default is 8ms.
	ListenerReadTimeoutMS time.Duration

	// SendChannelSize is the size of the channel used to send CAT commands to the serial port.
	//
	// Default is 10.
	SendChannelSize int

	// ProcessingChannelSize is the size of the channel used to receive a raw response line from the serial port.
	// It then processes the response line into a CAT status value that can be consumed by the frontend.
	//
	// Default is 10
	ProcessingChannelSize int
}
