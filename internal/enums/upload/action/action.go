package action

type Action string

const (
	Insert Action = "insert"
	Update Action = "update"
	Delete Action = "delete"
)

func (a Action) String() string {
	return string(a)
}

// Parse converts a string to an Action type
func Parse(s string) (Action, error) {
	switch s {
	case "insert":
		return Insert, nil
	case "update":
		return Update, nil
	case "delete":
		return Delete, nil
	default:
		return Insert, nil // Default to Insert for unknown values
	}
}
