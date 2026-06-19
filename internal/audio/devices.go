package audio

import "fmt"

// MatchDeviceName resolves a configured audio device name to its index in the
// live enumeration `names`. It is the shared resolver for capture and playback
// (dir is "capture" or "playback", used only in error text).
//
// Crucially it returns an error rather than silently first-matching when the
// name is AMBIGUOUS — two enumerated devices sharing the same name. Binding the
// wrong physical codec is worse than failing: for FT8 TX especially, PTT keys
// the configured CAT rig while the audio could stream to a different same-named
// device (review 2026-06-19 M1). On ambiguity the caller fails soft (that audio
// direction stays idle, logged), which is the safe outcome — never a wrong-codec
// transmission. The fix for the operator is to rename one device at the OS level
// (a durable per-device identifier is a future config-SPA item).
//
// CGO-free + pure, so it's unit-testable without a real audio backend; the
// CGO capture/playback layers just hand it the enumerated names.
func MatchDeviceName(names []string, want, dir string) (int, error) {
	idx, count := -1, 0
	for i, n := range names {
		if n == want {
			if idx < 0 {
				idx = i
			}
			count++
		}
	}
	switch {
	case count == 0:
		return -1, fmt.Errorf("%s device %q not found", dir, want)
	case count > 1:
		return -1, fmt.Errorf(
			"%s device name %q is ambiguous (%d devices share it); rename one at the OS level to disambiguate",
			dir, want, count)
	default:
		return idx, nil
	}
}
