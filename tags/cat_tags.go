package tags

type CatStateTag string

func (ca CatStateTag) String() string {
	return string(ca)
}

const (
	Identity CatStateTag = "IDENTITY"
	VfoAFreq CatStateTag = "VFOAFREQ"
	VfoBFreq CatStateTag = "VFOBFREQ"
	Split    CatStateTag = "SPLIT"
	Select   CatStateTag = "SELECT"
	MainMode CatStateTag = "MAINMODE"
	SubMode  CatStateTag = "SUBMODE"
	TxPwr    CatStateTag = "TXPWR"
)

var AllCatStateTags = []struct {
	Value  CatStateTag
	TSName string
}{
	{Value: Identity, TSName: Identity.String()},
	{Value: VfoAFreq, TSName: VfoAFreq.String()},
	{Value: VfoBFreq, TSName: VfoBFreq.String()},
	{Value: Split, TSName: Split.String()},
	{Value: Select, TSName: Select.String()},
	{Value: MainMode, TSName: MainMode.String()},
	{Value: SubMode, TSName: SubMode.String()},
	{Value: TxPwr, TSName: TxPwr.String()},
}
