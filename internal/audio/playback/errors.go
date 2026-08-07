package playback

import stderr "errors"

// Sentinel errors for the player lifecycle. Deliberately in a build-tag-free
// file (playback.go is cgo-gated): consumers classify these — the FT8 disarm
// path treats ErrNotPlaying from Stop() as the expected idle teardown, not a
// fault — and that classification must compile in the static, CGO-free build
// too, where the real device doesn't exist but the import still resolves.
var (
	ErrNotInitialized = stderr.New("audio playback not initialized")
	ErrAlreadyPlaying = stderr.New("audio playback already playing")
	ErrNotPlaying     = stderr.New("audio playback not playing")
	ErrClosed         = stderr.New("audio playback closed")
)
