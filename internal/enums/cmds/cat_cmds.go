package cmds

type CatCmdName string

func (cc CatCmdName) String() string {
	return string(cc)
}

const (
	Init     CatCmdName = "INIT"
	Read     CatCmdName = "READ"
	PlayBack CatCmdName = "PLAYBACK"
)
