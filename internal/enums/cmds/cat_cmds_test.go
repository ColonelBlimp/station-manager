package cmds

import "testing"

func TestCatCmdNameString(t *testing.T) {
	cases := map[CatCmdName]string{
		Init:     "INIT",
		Read:     "READ",
		PlayBack: "PLAYBACK",
	}

	for in, want := range cases {
		if got := in.String(); got != want {
			t.Fatalf("%q.String() = %q, want %q", in, got, want)
		}
	}
}
