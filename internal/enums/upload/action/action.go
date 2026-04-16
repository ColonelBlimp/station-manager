// Package action defines the upload queue action enum (insert, update, delete).
package action

import "fmt"

// Action represents the type of change that triggered an upload queue entry.
type Action string

const (
	Insert Action = "insert"
	Update Action = "update"
	Delete Action = "delete"
)

func (a Action) String() string {
	return string(a)
}

// Parse converts a string to an Action. Returns an error for unknown values.
func Parse(s string) (Action, error) {
	switch s {
	case "insert":
		return Insert, nil
	case "update":
		return Update, nil
	case "delete":
		return Delete, nil
	default:
		return "", fmt.Errorf("unknown upload action: %q", s)
	}
}
