# ptt

Package `ptt` controls a radio's push-to-talk line via a serial port control pin
(RTS or DTR). It is intentionally narrow in scope — it does nothing except drive
one hardware line high or low. Audio playback and CAT control are handled by
separate packages.

---

## Hardware background

Most radio-computer interfaces (SignaLink, homebrew PTT cables, CI-V adapters
with PTT) expose PTT through the **RTS** or **DTR** line of a serial port. The
port does not carry data; the control line alone keys the transmitter.

Common wiring:

| Interface          | Line to use |
|--------------------|-------------|
| SignaLink USB      | RTS         |
| Homebrew VOX cable | RTS or DTR  |
| CT-17 / CI-V clone | RTS         |

Check your interface documentation. If in doubt, RTS is the more common choice.

---

## Usage

### Basic RTS PTT

```go
import "github.com/ColonelBlimp/station-manager/internal/ptt"

p, err := ptt.Open(ptt.Config{
    PortName: "/dev/ttyUSB1",
    Line:     ptt.LineRTS,  // or ptt.LineDTR
})
if err != nil {
    // port not found, permissions issue, etc.
    log.Fatal(err)
}
defer p.Close()

// Key the transmitter
if err := p.Assert(); err != nil {
    log.Fatal(err)
}

// ... play audio, send CW, etc. ...

// Unkey
if err := p.Release(); err != nil {
    log.Fatal(err)
}
```

### DTR line

```go
p, err := ptt.Open(ptt.Config{
    PortName: "/dev/ttyUSB1",
    Line:     ptt.LineDTR,
})
```

### With structured logging

```go
p, err := ptt.Open(ptt.Config{
    PortName: "/dev/ttyUSB1",
    Line:     ptt.LineRTS,
    Logger:   myLogger, // implements logging.Logger; nil = no-op
})
```

### Contest CQ transmit sequence

`ptt` is designed to be composed with `audio.Playback` at the call site:

```go
if err := p.Assert(); err != nil {
    return err
}
defer p.Release() // always unkey, even on error

ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

return playback.PlayFile(ctx, "cq.wav")
```

---

## API reference

| Symbol | Description |
|---|---|
| `Open(cfg Config) (*PTT, error)` | Open the serial port and drive the line low. |
| `(*PTT).Assert() error` | Assert PTT (line high — TX). Idempotent. |
| `(*PTT).Release() error` | Release PTT (line low — RX). Idempotent. |
| `(*PTT).IsActive() bool` | Returns true while TX is asserted. |
| `(*PTT).Close() error` | Release PTT and close the port. Idempotent. |
| `ErrClosed` | Returned when any operation is attempted after `Close`. |

`Assert` and `Release` are **idempotent** — calling either twice in the same state
returns `nil`. `Close` is also idempotent.

---

## Configuration

```go
type Config struct {
    PortName string        // required — e.g. "/dev/ttyUSB1"
    Line     Line          // LineRTS (default) or LineDTR
    Logger   logging.Logger // nil = silent
}
```

The baud rate is set to 9600/8N1, which is conventional but irrelevant for
PTT-only ports since no data is transferred.

---

## Linux permissions

On most Linux distributions the serial port is owned by the `dialout` group:

```bash
sudo usermod -aG dialout $USER
# log out and back in for the change to take effect
```

Verify access:

```bash
ls -l /dev/ttyUSB1
# crw-rw---- 1 root dialout ...
```

---

## Testing

### Unit tests (no hardware required)

```bash
go test ./ptt/
go test -race ./ptt/
```

All unit tests use an in-memory mock port — no serial hardware is needed.

### Integration tests (real hardware)

```bash
# Default port /dev/ttyUSB1
go test -tags=integration ./ptt/ -v

# Override port via environment variable
PTT_TEST_PORT=/dev/ttyUSB0 go test -tags=integration ./ptt/ -v
```

The integration tests assert and release PTT with a 100ms hold between the two
so the line change can be observed on a multimeter or logic analyser.
