package sqlite

type Ordering string

const (
	Ascending  Ordering = "ASC"
	Descending Ordering = "DESC"
)

func (o Ordering) String() string {
	return string(o)
}

// sqlDir returns a SQL-safe ORDER BY direction — only ever "ASC" or "DESC",
// never the raw underlying string. Guards against a caller casting arbitrary
// text to Ordering and having it concatenated into an ORDER BY clause (an
// injection footgun); any value other than Descending maps to "ASC".
func (o Ordering) sqlDir() string {
	switch o {
	case Descending:
		return "DESC"
	default:
		return "ASC"
	}
}
