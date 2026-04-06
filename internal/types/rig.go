package types

type RigConfig struct {
	ID           int64        `json:"id"`
	Name         string       `json:"name"`
	Model        string       `json:"model"`
	Terminator   string       `json:"terminator"` // Terminator defines the character used to signal the end of a command.
	CatCommands  []CatCommand `json:"commands"`
	CatStates    []CatState   `json:"states"`
	SerialConfig SerialConfig `json:"serial_port"`
	CatConfig    CatConfig    `json:"cat"`
	PTTConfig    PTTConfig    `json:"ptt"`
}
