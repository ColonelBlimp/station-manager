package sqlite

type Ordering string

const (
	Ascending  Ordering = "ASC"
	Descending Ordering = "DESC"
)

var OrderingNames = []struct {
	Value  Ordering
	TSName string
}{
	{Value: Ascending, TSName: "ASC"},
	{Value: Descending, TSName: "DESC"},
}

func (o Ordering) String() string {
	return string(o)
}
