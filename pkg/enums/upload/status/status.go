package status

type Status string

const (
	Pending    Status = "pending"
	InProgress Status = "in_progress"
	Uploaded   Status = "uploaded"
	Failed     Status = "failed"
)

func (s Status) String() string {
	return string(s)
}
