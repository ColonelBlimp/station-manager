package events

type EventName string

const (
	Status EventName = "STATUS"
)

func (en EventName) String() string {
	return string(en)
}

var AllEvents = []struct {
	Value  EventName
	TSName string
}{
	{Value: Status, TSName: Status.String()},
}
