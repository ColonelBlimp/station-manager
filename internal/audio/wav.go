// internal/audio/wav.go
package audio

import (
	"encoding/binary"
	stderr "errors"
	"io"
	"math"
	"os"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

const (
	wavAudioFormatPCM   = 1
	wavAudioFormatFloat = 3
)

var (
	// ErrWAVInvalidHeader is returned when the file is not a valid RIFF/WAVE file.
	ErrWAVInvalidHeader = stderr.New("invalid WAV header")
	// ErrWAVUnsupportedFormat is returned for bit depths or formats not supported.
	ErrWAVUnsupportedFormat = stderr.New("unsupported WAV format")
)

// wavData holds the decoded contents of a WAV file.
type wavData struct {
	SampleRate uint32
	Channels   uint16
	Samples    []float32 // normalised to [-1.0, 1.0]
}

// readWAV opens a WAV file and decodes it to float32 samples.
// Supported formats: PCM 8-bit (unsigned), PCM 16-bit (signed), IEEE 754 float 32-bit.
// All channel interleaving is preserved; the caller is responsible for de-interleaving
// if mono downmix is needed.
func readWAV(path string) (*wavData, error) {
	const op errors.Op = "audio.readWAV"

	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}
	defer f.Close()

	// RIFF header: "RIFF" + file-size (LE uint32) + "WAVE"
	var riff [4]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return nil, errors.New(op).Err(ErrWAVInvalidHeader)
	}
	if string(riff[:]) != "RIFF" {
		return nil, errors.New(op).Err(ErrWAVInvalidHeader)
	}
	var fileSize uint32
	if err := binary.Read(f, binary.LittleEndian, &fileSize); err != nil {
		return nil, errors.New(op).Err(ErrWAVInvalidHeader)
	}
	var wave [4]byte
	if _, err := io.ReadFull(f, wave[:]); err != nil {
		return nil, errors.New(op).Err(ErrWAVInvalidHeader)
	}
	if string(wave[:]) != "WAVE" {
		return nil, errors.New(op).Err(ErrWAVInvalidHeader)
	}

	// Parse chunks until we locate "fmt " and "data".
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
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, errors.New(op).Err(err)
		}
		var chunkSize uint32
		if err := binary.Read(f, binary.LittleEndian, &chunkSize); err != nil {
			return nil, errors.New(op).Err(err)
		}

		switch string(chunkID[:]) {
		case "fmt ":
			if chunkSize < 16 {
				return nil, errors.New(op).Err(ErrWAVInvalidHeader)
			}
			if err := binary.Read(f, binary.LittleEndian, &audioFormat); err != nil {
				return nil, errors.New(op).Err(err)
			}
			if err := binary.Read(f, binary.LittleEndian, &channels); err != nil {
				return nil, errors.New(op).Err(err)
			}
			if err := binary.Read(f, binary.LittleEndian, &sampleRate); err != nil {
				return nil, errors.New(op).Err(err)
			}
			// Skip byteRate (4 bytes) and blockAlign (2 bytes)
			if _, err := io.CopyN(io.Discard, f, 6); err != nil {
				return nil, errors.New(op).Err(err)
			}
			if err := binary.Read(f, binary.LittleEndian, &bitsPerSample); err != nil {
				return nil, errors.New(op).Err(err)
			}
			// Skip any extra fmt bytes (e.g. extensible format extra fields)
			if extra := int64(chunkSize) - 16; extra > 0 {
				if _, err := io.CopyN(io.Discard, f, extra); err != nil {
					return nil, errors.New(op).Err(err)
				}
			}
			fmtFound = true

		case "data":
			if !fmtFound {
				return nil, errors.New(op).Msg("data chunk precedes fmt chunk")
			}
			// Use LimitReader rather than pre-allocating chunkSize bytes: a corrupt or
			// adversarial file could declare chunkSize = 0xFFFFFFFF and OOM the process
			// before io.ReadFull even attempts to read. LimitReader caps the read to the
			// declared size but only allocates as fast as data actually arrives.
			var readErr error
			pcmData, readErr = io.ReadAll(io.LimitReader(f, int64(chunkSize)))
			if readErr != nil {
				return nil, errors.New(op).Err(readErr)
			}
			break outer

		default:
			// Unknown chunk — skip, honouring the RIFF word-alignment padding rule.
			skip := int64(chunkSize)
			if chunkSize%2 != 0 {
				skip++
			}
			if _, err := io.CopyN(io.Discard, f, skip); err != nil && err != io.EOF {
				return nil, errors.New(op).Err(err)
			}
		}
	}

	if !fmtFound || pcmData == nil {
		return nil, errors.New(op).Err(ErrWAVInvalidHeader)
	}

	samples, err := convertWAVSamples(audioFormat, bitsPerSample, pcmData)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}

	return &wavData{
		SampleRate: sampleRate,
		Channels:   channels,
		Samples:    samples,
	}, nil
}

// convertWAVSamples converts raw PCM bytes to normalised float32 samples.
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
		// 8-bit WAV is unsigned: 0=min, 128=silence, 255=max
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
