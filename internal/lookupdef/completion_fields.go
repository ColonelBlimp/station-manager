package lookupdef

// CompletionField describes one callsign-owned station field that may gate a
// lower-priority lookup (ADR 0068). The daemon serves this catalogue to
// Settings; config validation and the orchestrator use the same stable names.
type CompletionField struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

const (
	CompletionFieldName       = "name"
	CompletionFieldGridsquare = "gridsquare"
)

var completionFields = []CompletionField{
	{Name: CompletionFieldName, DisplayName: "Name"},
	{Name: CompletionFieldGridsquare, DisplayName: "Gridsquare"},
}

// CompletionFields returns the supported fields in stable UI order.
func CompletionFields() []CompletionField {
	return append([]CompletionField(nil), completionFields...)
}

// DefaultCompletionFields is ADR 0068's initial "complete enough" policy.
func DefaultCompletionFields() []string {
	out := make([]string, 0, len(completionFields))
	for _, f := range completionFields {
		out = append(out, f.Name)
	}
	return out
}

// IsCompletionField reports whether name is supported by this build.
func IsCompletionField(name string) bool {
	for _, f := range completionFields {
		if f.Name == name {
			return true
		}
	}
	return false
}
