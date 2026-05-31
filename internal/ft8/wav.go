package ft8

import (
	"encoding/binary"
	"io"
	"os"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

const opReadSlotWAV errors.Op = "ft8.readSlotWAV"

// readSlotWAV reads a WAV file into the int16 slot go-ft8 decodes. It
// enforces go-ft8's input contract — 12 kHz, mono, signed-16-bit PCM — and
// returns a descriptive error rather than silently mis-decoding a file that
// doesn't meet it. Resampling/downmixing a non-conforming source is the
// caller's job (and a future audio-front-end concern); this reader is the
// straight-through int16 seam, not a converter.
//
// Chunk-walking (rather than assuming the canonical 44-byte layout) so files
// carrying LIST/JUNK/fact chunks still read. The data chunk's declared size
// is fed through io.LimitReader so a corrupt oversized size can't pre-allocate
// the process to death.
func readSlotWAV(path string) ([]int16, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New(opReadSlotWAV).WithErr(err)
	}
	defer func() { _ = f.Close() }()

	var riff [4]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("read RIFF tag")
	}
	if string(riff[:]) != "RIFF" {
		return nil, errors.New(opReadSlotWAV).WithMsg("not a RIFF file")
	}
	if _, err := io.CopyN(io.Discard, f, 4); err != nil { // RIFF chunk size
		return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("read RIFF size")
	}
	var wave [4]byte
	if _, err := io.ReadFull(f, wave[:]); err != nil {
		return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("read WAVE tag")
	}
	if string(wave[:]) != "WAVE" {
		return nil, errors.New(opReadSlotWAV).WithMsg("not a WAVE file")
	}

	var (
		audioFormat   uint16
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16
		pcm           []byte
		fmtSeen       bool
	)

	for {
		var id [4]byte
		if _, err := io.ReadFull(f, id[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("read chunk id")
		}
		var size uint32
		if err := binary.Read(f, binary.LittleEndian, &size); err != nil {
			return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("read chunk size")
		}

		switch string(id[:]) {
		case "fmt ":
			fields := struct {
				AudioFormat   uint16
				Channels      uint16
				SampleRate    uint32
				ByteRate      uint32
				BlockAlign    uint16
				BitsPerSample uint16
			}{}
			if err := binary.Read(f, binary.LittleEndian, &fields); err != nil {
				return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("read fmt chunk")
			}
			audioFormat = fields.AudioFormat
			channels = fields.Channels
			sampleRate = fields.SampleRate
			bitsPerSample = fields.BitsPerSample
			fmtSeen = true
			// Skip any extension bytes beyond the 16-byte PCM fmt body.
			if size > 16 {
				if _, err := io.CopyN(io.Discard, f, int64(size-16)); err != nil {
					return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("skip fmt extension")
				}
			}
		case "data":
			buf, err := io.ReadAll(io.LimitReader(f, int64(size)))
			if err != nil {
				return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("read data chunk")
			}
			pcm = buf
		default:
			if _, err := io.CopyN(io.Discard, f, int64(size)); err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					break
				}
				return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("skip chunk")
			}
		}
		// RIFF chunks are word-aligned: an odd size carries a pad byte.
		if size%2 == 1 {
			if _, err := io.CopyN(io.Discard, f, 1); err != nil && err != io.EOF {
				return nil, errors.New(opReadSlotWAV).WithErr(err).WithMsg("skip pad byte")
			}
		}
	}

	if !fmtSeen {
		return nil, errors.New(opReadSlotWAV).WithMsg("no fmt chunk")
	}
	if pcm == nil {
		return nil, errors.New(opReadSlotWAV).WithMsg("no data chunk")
	}
	if audioFormat != 1 {
		return nil, errors.New(opReadSlotWAV).WithMsgf("unsupported audio format %d (want PCM=1)", audioFormat)
	}
	if int(sampleRate) != goft8.SampleRate {
		return nil, errors.New(opReadSlotWAV).WithMsgf("sample rate %d Hz (want %d)", sampleRate, goft8.SampleRate)
	}
	if int(channels) != goft8.Channels {
		return nil, errors.New(opReadSlotWAV).WithMsgf("%d channels (want %d, mono)", channels, goft8.Channels)
	}
	if int(bitsPerSample) != goft8.BitsPerSample {
		return nil, errors.New(opReadSlotWAV).WithMsgf("%d bits/sample (want %d)", bitsPerSample, goft8.BitsPerSample)
	}

	n := len(pcm) / 2
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	return out, nil
}
