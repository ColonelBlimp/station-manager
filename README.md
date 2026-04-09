<p align="left">
  <img src="assets/logo.png" alt="Station Manager" width="128">
</p>

# Station Manager

**Station Manager** is a suite of modern Linux desktop applications for Amateur Radio station management. They are built using
Go, SvelteKit with the Wails framework binding it all together. Distribution is currently via `.rpm` and `.deb` packages.

Why yet another piece of software for amateur radio logging, etc.? Well, what is out there just doesn't allow me to
operate in the way I want to. Also, I don't generally use Windows, and I don't want to use Mac,
so I was left with writing the software myself. Besides, many packages out there, while working, look way
out-of-date, cost too much, and their UIs are far too busy to make them easy to set up and a joy to use
(opinionated – as this software is also).

One of the other main requirements is that the software should not require an internet connection to operate.
Here in Malawi, the internet is not always available, and when it is, it is not always reliable. So, the software should
be able to operate without an internet connection. The application will forward QSOs to online logbooks such as QRZ.com, ClubLog,
Station Manager (all configurable), but this is not a requirement for the software to operate.

The software is not aimed at contests (although it does support contesting), rather at general HF operation by SSB, CW,
and FT8.

## Computer Aided Transceiver (CAT)

The software does support CAT operation; however, only Yaesu FTdx10 and F7-100 have been tested (I don't own any other
rigs).

## FT8/FT4

The code base contains a native Go implementation of the FT8 protocol stack,
working towards a complete FT8 digital mode without external dependencies
(no WSJT-X required).

| Component | Package | Status |
|---|---|---|
| Audio I/O (capture + playback) | `internal/audio/` | ✅ Complete |
| WAV file reader | `internal/audio/wav.go` | ✅ Complete |
| Window timing (TX/RX boundaries) | `internal/ft8/timing/` | ✅ Complete |
| Message pack/unpack + CRC-14 | `internal/ft8/message/` | ✅ Complete |
| LDPC codec (encoder + decoder) | `internal/ft8/codec/` | ✅ Complete |
| DSP pipeline (FFT, spectrogram, candidate detection, soft demodulation) | `internal/ft8/dsp/` | ✅ Complete |
| TX synthesis (GFSK) | `internal/ft8/synth/` | ✅ Complete |
| In-memory audio playback (PlaySamples) | `internal/audio/` | ✅ Complete |
| FT8 service (RX orchestration) | `internal/ft8/service/` | 🔧 In Progress |
| FT8 service (TX orchestration + QSO state machine) | `internal/ft8/service/` | Planned |

The full **RX decode chain** is functional end-to-end: audio samples →
spectrogram → candidate detection → soft demodulation → LDPC decode → CRC
verify → decoded messages. The full **TX audio chain** is also complete:
message → LDPC encode → symbol mapping → GFSK synthesis → PlaySamples.
The FT8 service that orchestrates both paths is under active development,
starting with the RX-only loop (audio capture → window accumulation →
decode → output).
