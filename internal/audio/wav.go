// Package audio provides CGO-free audio file I/O and FFT primitives.
// ReadWAV/WriteWAV are the file-I/O pair; the FFT helpers live in
// fft.go / realfft.go.
//
// The package is deliberately self-contained with no daemon-subsystem
// dependencies, so it stays open for future consumers (e.g. operator-
// facing recording / playback for QSO archives). It was retained when
// the FT8 subsystem was moved out of the SM tree (2026-05-30, preserved
// at tag ft8-snapshot-2026-05-30); the former audio/capture CGO
// miniaudio subpackage went with that removal.
package audio

import (
	"bufio"
	"encoding/binary"
	stderrors "errors"
	"io"
	"math"
	"os"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

const (
	wavAudioFormatPCM   = 1
	wavAudioFormatFloat = 3
)

// ErrWAVInvalidHeader is returned when the file isn't a valid
// RIFF/WAVE file or its chunk structure is malformed.
var ErrWAVInvalidHeader = stderrors.New("audio: invalid WAV header")

// ErrWAVUnsupportedFormat is returned for bit depths or audio formats
// the reader doesn't decode. Supported: PCM 8-bit (unsigned), PCM
// 16-bit (signed), IEEE 754 float 32-bit. PCM 24-bit and PCM 32-bit
// integer are out of scope (FT8 captures from WSJT-X and ft8sim are
// 16-bit signed at 12 kHz; the other supported widths cover ad-hoc
// recordings).
var ErrWAVUnsupportedFormat = stderrors.New("audio: unsupported WAV format")

// Data is a decoded WAV file. Samples are normalised to [-1.0, 1.0]
// (float32) and channel-interleaved exactly as the source file stored
// them. Mono files have one sample per frame; stereo files have two
// interleaved samples per frame. Callers that want a mono downmix or
// a single-channel view of a stereo file do that themselves.
type Data struct {
	SampleRate uint32
	Channels   uint16
	Samples    []float32
}

// ReadWAV opens a WAV file and decodes its samples to normalised
// float32. Returns ErrWAVInvalidHeader if the file's RIFF/WAVE
// structure is malformed, or ErrWAVUnsupportedFormat for sample
// formats outside the supported set (see ErrWAVUnsupportedFormat
// for the catalogue).
//
// **Defensive against adversarial chunk sizes.** The "data" chunk's
// declared size is fed through io.LimitReader rather than used to
// pre-allocate a slice — a corrupt or hostile file declaring
// chunkSize = 0xFFFFFFFF would otherwise OOM the process before
// any read attempt. LimitReader caps the read to the declared size
// but only allocates as fast as data actually arrives, capped by
// the actual file length.
func ReadWAV(path string) (*Data, error) {
	const op errors.Op = "audio.ReadWAV"

	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	defer func() { _ = f.Close() }()

	// RIFF header: "RIFF" + file-size (LE uint32) + "WAVE".
	var riff [4]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return nil, errors.New(op).WithErr(ErrWAVInvalidHeader)
	}
	if string(riff[:]) != "RIFF" {
		return nil, errors.New(op).WithErr(ErrWAVInvalidHeader)
	}
	var fileSize uint32
	if err := binary.Read(f, binary.LittleEndian, &fileSize); err != nil {
		return nil, errors.New(op).WithErr(ErrWAVInvalidHeader)
	}
	_ = fileSize // declared but not validated against actual file length — chunk-bounded reads handle that
	var wave [4]byte
	if _, err := io.ReadFull(f, wave[:]); err != nil {
		return nil, errors.New(op).WithErr(ErrWAVInvalidHeader)
	}
	if string(wave[:]) != "WAVE" {
		return nil, errors.New(op).WithErr(ErrWAVInvalidHeader)
	}

	// Walk the chunk stream until "fmt " (header metadata) and "data"
	// (the sample payload) are both seen. Unknown chunks (LIST, JUNK,
	// etc.) are skipped per the RIFF word-alignment padding rule.
	var (
		audioFormat   uint16
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16
		pcmData       []byte
		fmtFound      bool
	)

outer:
	for {
		var chunkID [4]byte
		if _, err := io.ReadFull(f, chunkID[:]); err != nil {
			if stderrors.Is(err, io.EOF) || stderrors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, errors.New(op).WithErr(err)
		}
		var chunkSize uint32
		if err := binary.Read(f, binary.LittleEndian, &chunkSize); err != nil {
			return nil, errors.New(op).WithErr(err)
		}

		switch string(chunkID[:]) {
		case "fmt ":
			if chunkSize < 16 {
				return nil, errors.New(op).WithErr(ErrWAVInvalidHeader)
			}
			if err := binary.Read(f, binary.LittleEndian, &audioFormat); err != nil {
				return nil, errors.New(op).WithErr(err)
			}
			if err := binary.Read(f, binary.LittleEndian, &channels); err != nil {
				return nil, errors.New(op).WithErr(err)
			}
			if err := binary.Read(f, binary.LittleEndian, &sampleRate); err != nil {
				return nil, errors.New(op).WithErr(err)
			}
			// Skip byteRate (4 bytes) and blockAlign (2 bytes) — derivable
			// from the fields above.
			if _, err := io.CopyN(io.Discard, f, 6); err != nil {
				return nil, errors.New(op).WithErr(err)
			}
			if err := binary.Read(f, binary.LittleEndian, &bitsPerSample); err != nil {
				return nil, errors.New(op).WithErr(err)
			}
			// Skip any extra fmt bytes (extensible-format extra fields, etc.).
			if extra := int64(chunkSize) - 16; extra > 0 {
				if _, err := io.CopyN(io.Discard, f, extra); err != nil {
					return nil, errors.New(op).WithErr(err)
				}
			}
			fmtFound = true

		case "data":
			if !fmtFound {
				return nil, errors.New(op).WithMsg("data chunk precedes fmt chunk")
			}
			var readErr error
			pcmData, readErr = io.ReadAll(io.LimitReader(f, int64(chunkSize)))
			if readErr != nil {
				return nil, errors.New(op).WithErr(readErr)
			}
			break outer

		default:
			// Unknown chunk — skip, honouring the RIFF word-alignment
			// padding rule (odd-sized chunks are followed by a single
			// pad byte).
			skip := int64(chunkSize)
			if chunkSize%2 != 0 {
				skip++
			}
			if _, err := io.CopyN(io.Discard, f, skip); err != nil && !stderrors.Is(err, io.EOF) {
				return nil, errors.New(op).WithErr(err)
			}
		}
	}

	if !fmtFound || pcmData == nil {
		return nil, errors.New(op).WithErr(ErrWAVInvalidHeader)
	}

	samples, err := convertWAVSamples(audioFormat, bitsPerSample, pcmData)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}

	return &Data{
		SampleRate: sampleRate,
		Channels:   channels,
		Samples:    samples,
	}, nil
}

// WriteWAV writes d to a 16-bit signed PCM WAV file at path — the
// inverse of ReadWAV's 16-bit branch and the format the existing FT8
// fixtures (ft8_cap*.wav) use. Float32 samples are clamped to
// [-1.0, 1.0] and scaled by 32767 (so exactly +1.0 maps to the int16
// ceiling rather than wrapping). Channel interleaving is taken from
// d.Samples verbatim; the caller owns the mono/stereo decision.
//
// Float32-source captures lose sub-int16 precision in the round-trip,
// which is inaudible and irrelevant to FT8 decoding (WSJT-X itself
// stores slots as 16-bit) — the win is a fixture identical in format
// to the baked-in corpus, decodable by the same ReadWAV path.
func WriteWAV(path string, d *Data) error {
	const op errors.Op = "audio.WriteWAV"

	if d == nil {
		return errors.New(op).WithMsg("nil audio data")
	}
	channels := d.Channels
	if channels == 0 {
		channels = 1
	}

	const bitsPerSample = 16
	const bytesPerSample = bitsPerSample / 8
	dataSize := len(d.Samples) * bytesPerSample
	byteRate := d.SampleRate * uint32(channels) * bytesPerSample
	blockAlign := channels * bytesPerSample

	f, err := os.Create(path)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	defer func() { _ = f.Close() }()

	w := bufio.NewWriter(f)

	// RIFF/WAVE container + canonical 16-byte PCM "fmt " chunk + "data".
	hdr := []any{
		[]byte("RIFF"),
		uint32(36 + dataSize), // chunk size: everything after this field
		[]byte("WAVE"),
		[]byte("fmt "),
		uint32(16),                // PCM fmt chunk body size
		uint16(wavAudioFormatPCM), // audioFormat = 1
		channels,
		d.SampleRate,
		byteRate,
		blockAlign,
		uint16(bitsPerSample),
		[]byte("data"),
		uint32(dataSize),
	}
	for _, field := range hdr {
		if b, ok := field.([]byte); ok {
			if _, err := w.Write(b); err != nil {
				return errors.New(op).WithErr(err)
			}
			continue
		}
		if err := binary.Write(w, binary.LittleEndian, field); err != nil {
			return errors.New(op).WithErr(err)
		}
	}

	var buf [2]byte
	for _, s := range d.Samples {
		v := int32(math.Round(float64(s) * 32767.0))
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		binary.LittleEndian.PutUint16(buf[:], uint16(int16(v)))
		if _, err := w.Write(buf[:]); err != nil {
			return errors.New(op).WithErr(err)
		}
	}

	if err := w.Flush(); err != nil {
		return errors.New(op).WithErr(err)
	}
	return nil
}

// convertWAVSamples converts the raw PCM byte payload to normalised
// float32. The three supported (audioFormat, bitsPerSample) tuples
// are pinned in the switch; everything else returns
// ErrWAVUnsupportedFormat.
//
// Normalisation:
//   - PCM 16-bit signed: int16 sample / 32768.0 → [-1.0, 1.0).
//   - PCM 8-bit unsigned: (uint8 - 128) / 128.0 → [-1.0, ~0.992].
//   - IEEE float 32-bit: passes through (file format already
//     normalised by the producer).
func convertWAVSamples(audioFormat, bitsPerSample uint16, data []byte) ([]float32, error) {
	switch {
	case audioFormat == wavAudioFormatPCM && bitsPerSample == 16:
		if len(data)%2 != 0 {
			return nil, ErrWAVInvalidHeader
		}
		samples := make([]float32, len(data)/2)
		for i := range samples {
			v := int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
			samples[i] = float32(v) / 32768.0
		}
		return samples, nil

	case audioFormat == wavAudioFormatPCM && bitsPerSample == 8:
		// 8-bit WAV is unsigned: 0=min, 128=silence, 255=max.
		samples := make([]float32, len(data))
		for i, b := range data {
			samples[i] = (float32(b) - 128.0) / 128.0
		}
		return samples, nil

	case audioFormat == wavAudioFormatFloat && bitsPerSample == 32:
		if len(data)%4 != 0 {
			return nil, ErrWAVInvalidHeader
		}
		samples := make([]float32, len(data)/4)
		for i := range samples {
			bits := binary.LittleEndian.Uint32(data[i*4 : i*4+4])
			samples[i] = math.Float32frombits(bits)
		}
		return samples, nil

	default:
		return nil, ErrWAVUnsupportedFormat
	}
}
