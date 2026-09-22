package archive

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestForwardingAdmitted_OnlyTheAdoptedArchive(t *testing.T) {
	cases := []struct {
		name  string
		entry *types.QsoArchiveConfig
		want  bool
	}{
		{"not yet adopted (no entry)", nil, true},
		{"the adopted file", &types.QsoArchiveConfig{ID: idA, Ownership: types.QsoArchiveOwnershipLegacy}, true},
		{"a managed archive", &types.QsoArchiveConfig{ID: idB, Ownership: types.QsoArchiveOwnershipManaged}, false},
		{"an external archive", &types.QsoArchiveConfig{ID: idB, Ownership: types.QsoArchiveOwnershipExternal}, false},
	}
	for _, tc := range cases {
		if got := ForwardingAdmitted(tc.entry); got != tc.want {
			t.Errorf("%s: admitted = %v, want %v", tc.name, got, tc.want)
		}
	}
}
