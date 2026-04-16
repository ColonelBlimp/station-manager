package sqlite

type Ordering string

const (
	Ascending  Ordering = "ASC"
	Descending Ordering = "DESC"
)

func (o Ordering) String() string {
	return string(o)
}
