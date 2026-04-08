# audio

Package `audio` provides real-time audio capture and WAV file playback via
[miniaudio](https://miniaud.io/) (through the `github.com/gen2brain/malgo` CGo binding).
It is a pure I/O layer with no station-manager service dependencies.

Requires CGo and a C compiler. On Fedora: `sudo dnf install gcc`.

---

## Overview

| Type | Purpose |
|---|---|
| `Capture` | Records audio from a microphone or line-in device |
| `Playback` | Plays a WAV file to an output device |

Both types share the same `Config` struct and `DefaultConfig()` constructor.
They are independent — you can use either without the other.

---

## Shared Config

```go
type Config struct {
    DeviceIndex int            // -1 = default device
    SampleRate  uint32         // used by Capture only; Playback reads rate from WAV
    Channels    uint32         // used by Capture only; Playback reads channels from WAV
    BufferSize  uint32         // frames per audio callback
    Logger      logging.Logger // nil = no-op
}

func DefaultConfig() Config // {-1, 48000, 1, 512, nil}
```

---

## Capture

### Basic usage

```go
import "github.com/ColonelBlimp/station-manager/internal/audio"

c := audio.New(audio.DefaultConfig())
defer c.Close()

if err := c.Init(); err != nil {
    log.Fatal(err)
}

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := c.Start(ctx); err != nil {
    log.Fatal(err)
}

for samples := range c.Samples() {
    // samples is []float32 normalised to [-1.0, 1.0]
    processSamples(samples)
}
```

`Start` is non-blocking. Capture runs until the context is cancelled, `Stop()` is called,
or `Close()` is called. The `Samples()` channel is closed when `Close()` is called.

### Real-time callback (low latency)

```go
c.SetCallback(func(samples []float32) {
    // Called directly from the audio thread — must be fast and non-blocking.
    // WARNING: samples is only valid for the duration of this call; copy if needed.
    peak := findPeak(samples)
    atomic.StoreInt64(&peakVal, int64(peak*1000))
})
```

Use `SetCallback` for processing that cannot tolerate channel buffering latency.
Use `c.Samples()` for everything else.

### Listing devices

```go
if err := c.Init(); err != nil {
    log.Fatal(err)
}
devices, err := c.ListDevices()
for i, d := range devices {
    fmt.Printf("[%d] %s\n", i, d.Name)
}
// Pass the index you want via Config.DeviceIndex
```

### API reference

| Symbol | Description |
|---|---|
| `New(cfg Config) *Capture` | Create a Capture (not yet started) |
| `(*Capture).Init() error` | Initialise the audio backend |
| `(*Capture).ListDevices() ([]malgo.DeviceInfo, error)` | Enumerate capture devices |
| `(*Capture).Start(ctx context.Context) error` | Begin capture (non-blocking) |
| `(*Capture).Stop() error` | Stop capture |
| `(*Capture).Close() error` | Stop capture and release all resources |
| `(*Capture).IsRunning() bool` | True while capture is active |
| `(*Capture).Samples() <-chan []float32` | Channel of captured sample buffers |
| `(*Capture).SetCallback(SampleCallback)` | Register a real-time callback (nil to remove) |
| `ErrNotInitialized` | Init not called |
| `ErrAlreadyRunning` | Start called while already running |
| `ErrNotRunning` | Stop called while not running |
| `ErrClosed` | Operation attempted after Close |

---

## Playback

### Basic usage

```go
p := audio.NewPlayback(audio.DefaultConfig())
defer p.Close()

if err := p.Init(); err != nil {
    log.Fatal(err)
}

ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := p.PlayFile(ctx, "cq.wav"); err != nil {
    log.Fatal(err)
}
```

`PlayFile` blocks until the file finishes, the context is cancelled, or `Stop()`/`Close()`
is called. Only one file can play at a time; concurrent calls return
`ErrPlaybackAlreadyPlaying`.

The device sample rate and channel count are read from the WAV file header, so
`Config.SampleRate` and `Config.Channels` are ignored for playback.

### Stop mid-play

```go
go func() {
    time.Sleep(3 * time.Second)
    _ = p.Stop()
}()
p.PlayFile(ctx, "long-cq.wav") // returns after Stop() is called
```

### With PTT (typical contest CQ sequence)

```go
if err := ptt.Assert(); err != nil {
    return err
}
defer ptt.Release()

ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

return playback.PlayFile(ctx, "cq.wav")
```

### Listing playback devices

```go
devices, err := p.ListDevices()
for i, d := range devices {
    fmt.Printf("[%d] %s\n", i, d.Name)
}
// Use the index in Config.DeviceIndex when constructing Playback
cfg := audio.DefaultConfig()
cfg.DeviceIndex = 2
p := audio.NewPlayback(cfg)
```

### API reference

| Symbol | Description |
|---|---|
| `NewPlayback(cfg Config) *Playback` | Create a Playback (not yet initialised) |
| `(*Playback).Init() error` | Initialise the audio backend |
| `(*Playback).ListDevices() ([]malgo.DeviceInfo, error)` | Enumerate playback devices |
| `(*Playback).PlayFile(ctx, path) error` | Play a WAV file (blocking) |
| `(*Playback).Stop() error` | Interrupt in-progress playback |
| `(*Playback).Close() error` | Stop playback and release all resources |
| `(*Playback).IsPlaying() bool` | True while PlayFile is in progress |
| `ErrPlaybackNotInitialized` | Init not called |
| `ErrPlaybackAlreadyPlaying` | PlayFile called while already playing |
| `ErrPlaybackNotPlaying` | Stop called while nothing is playing |
| `ErrPlaybackClosed` | Operation attempted after Close |

---

## WAV support

`PlayFile` reads WAV files internally. The exported `ReadWAV` function is also
available for other packages that need to decode WAV files (e.g., the FT8 DSP
pipeline tests).

```go
wav, err := audio.ReadWAV("recording.wav")
// wav.SampleRate, wav.Channels, wav.Samples ([]float32)
```

Supported formats:

| Format | Bit depth |
|---|---|
| PCM (format 1) | 8-bit unsigned, 16-bit signed |
| IEEE 754 float (format 3) | 32-bit |

Stereo and multi-channel files are supported; interleaving is preserved.
The file is fully decoded into memory before playback starts.

`readWAV` uses `io.LimitReader` rather than pre-allocating the full chunk size,
so a corrupt file declaring `chunkSize = 0xFFFFFFFF` will not OOM the process.

| Symbol | Type | Description |
|---|---|---|
| `ReadWAV(path) (*WAVData, error)` | Function | Decode a WAV file to `[]float32` |
| `WAVData` | Struct | Holds `SampleRate`, `Channels`, `Samples` |
| `ErrWAVInvalidHeader` | Error | Not a valid RIFF/WAVE file |
| `ErrWAVUnsupportedFormat` | Error | Bit depth or format not supported |

---

## Linux permissions

Serial and audio devices may require group membership:

```bash
sudo usermod -aG audio $USER
# log out and back in for the change to take effect
```

Verify:

```bash
ls -l /dev/snd/
```

---

## Testing

### Unit tests (no hardware required)

```bash
cd internal
go test ./audio/
go test -race ./audio/
```

All unit tests use in-memory mocks — no audio hardware is needed.

### Integration tests (real hardware)

```bash
cd internal
go test -tags=integration ./audio/ -v
```

Integration tests require audio input and output devices. They play a 1-second
440 Hz sine wave and record live input. The playback test asserts that `PlayFile`
takes longer than 200 ms to return (guards against audio callback never firing).

---

## Notes

- A `Capture` or `Playback` cannot be reused after `Close()` — create a new instance.
- `Playback` waits 500 ms after the last sample is submitted before stopping the device.
  This allows PipeWire/ALSA pipeline buffers to drain (~150–250 ms typical latency).
- The `Samples()` channel has a buffer of 64 frames. If the consumer is too slow, frames
  are dropped silently (the capture callback never blocks).
