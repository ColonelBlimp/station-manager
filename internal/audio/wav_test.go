package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// buildWAV writes a minimal RIFF/WAVE file to a temp directory and
// returns its path. Builder for the canonical 3-chunk shape (RIFF
// header → fmt chunk → data chunk); buildWAVWithExtraChunk covers
// the unknown-chunk-skip path separately.
//
//   - audioFormat: 1 = PCM, 3 = IEEE float (wavAudioFormatPCM /
//     wavAudioFormatFloat).
//   - bitsPerSample: 8, 16, or 32 (24 + 32-int are deliberately
//     out of scope to exercise ErrWAVUnsupportedFormat).
//   - data: raw PCM bytes for the data chunk, little-endian, in the
//     bit width declared above.
func buildWAV(t *testing.T, audioFormat, channels uint16, sampleRate uint32, bitsPerSample uint16, data []byte) string {
	t.Helper()

	fmtChunkSize := uint32(16)
	dataChunkSize := uint32(len(data))
	// "WAVE" + ("fmt " + chunkSize + 16 fmt bytes) + ("data" + chunkSize + dataChunkSize).
	fileSize := 4 + 8 + fmtChunkSize + 8 + dataChunkSize

	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	write := func(v any) {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	writeStr := func(s string) {
		if _, err := f.WriteString(s); err != nil {
			t.Fatalf("writeStr: %v", err)
		}
	}

	writeStr("RIFF")
	write(uint32(fileSize))
	writeStr("WAVE")

	writeStr("fmt ")
	write(fmtChunkSize)
	write(audioFormat)
	write(channels)
	write(sampleRate)
	write(uint32(uint32(sampleRate) * uint32(channels) * uint32(bitsPerSample) / 8)) // byteRate
	write(uint16(uint16(channels) * bitsPerSample / 8))                              // blockAlign
	write(bitsPerSample)

	writeStr("data")
	write(dataChunkSize)
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write data: %v", err)
	}

	return f.Name()
}

// buildWAVWithExtraChunk inserts an unknown chunk (LIST) before the
// data chunk so we exercise the unknown-chunk-skip path in ReadWAV.
// The LIST payload is exactly 4 bytes so the chunk is naturally
// word-aligned (no pad byte needed); a separate test exercises the
// odd-size pad path.
func buildWAVWithExtraChunk(t *testing.T) string {
	t.Helper()
	// Two PCM16 samples: 0x0000 (silence), 0x7FFF (max positive).
	pcm := []byte{0x00, 0x00, 0xFF, 0x7F}

	extra := []byte("LIST\x04\x00\x00\x00INFO") // 8-byte header + 4-byte payload
	fmtSize := uint32(16)
	extraSize := uint32(len(extra))
	dataSize := uint32(len(pcm))
	fileSize := 4 + 8 + fmtSize + extraSize + 8 + dataSize

	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	write := func(v any) {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	writeStr := func(s string) {
		if _, err := f.WriteString(s); err != nil {
			t.Fatalf("writeStr: %v", err)
		}
	}

	writeStr("RIFF")
	write(uint32(fileSize))
	writeStr("WAVE")

	writeStr("fmt ")
	write(fmtSize)
	write(uint16(1))     // PCM
	write(uint16(1))     // mono
	write(uint32(44100)) // sample rate
	write(uint32(88200)) // byte rate
	write(uint16(2))     // block align
	write(uint16(16))    // bits per sample

	// Extra unknown chunk (LIST with 4-byte INFO payload).
	writeStr("LIST")
	write(uint32(4))
	writeStr("INFO")

	writeStr("data")
	write(dataSize)
	if _, err := f.Write(pcm); err != nil {
		t.Fatalf("write data: %v", err)
	}

	return f.Name()
}

// pcm16Bytes encodes float32 samples ([-1, 1]) as little-endian int16
// PCM bytes — the canonical FT8 capture format used by ft8sim and
// WSJT-X. 32767 (not 32768) on the high end matches the asymmetric
// int16 range.
func pcm16Bytes(samples []float32) []byte {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		v := int16(s * 32767)
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}
	return buf
}

// float32Bytes encodes float32 samples as little-endian IEEE 754 bytes
// for the audioFormat=3 path.
func float32Bytes(samples []float32) []byte {
	buf := make([]byte, len(samples)*4)
	for i, s := range samples {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(s))
	}
	return buf
}

// --------------- ReadWAV — valid files ----------------------------------------

func TestReadWAV_PCM16_Mono(t *testing.T) {
	input := []float32{0.0, 1.0, -1.0, 0.5}
	path := buildWAV(t, wavAudioFormatPCM, 1, 44100, 16, pcm16Bytes(input))

	w, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if w.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", w.SampleRate)
	}
	if w.Channels != 1 {
		t.Errorf("Channels = %d, want 1", w.Channels)
	}
	if len(w.Samples) != 4 {
		t.Fatalf("len(Samples) = %d, want 4", len(w.Samples))
	}
	for i, want := range input {
		// PCM16 normalisation has a ~1/32768 quantization step;
		// 1e-3 tolerance comfortably covers it.
		if math.Abs(float64(w.Samples[i]-want)) > 1e-3 {
			t.Errorf("Samples[%d] = %g, want %g", i, w.Samples[i], want)
		}
	}
}

func TestReadWAV_PCM16_Stereo(t *testing.T) {
	// 2 stereo frames = 4 interleaved samples.
	input := []float32{0.1, 0.2, 0.3, 0.4}
	path := buildWAV(t, wavAudioFormatPCM, 2, 48000, 16, pcm16Bytes(input))

	w, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if w.Channels != 2 {
		t.Errorf("Channels = %d, want 2", w.Channels)
	}
	if len(w.Samples) != 4 {
		t.Errorf("len(Samples) = %d, want 4 (interleaved L/R/L/R)", len(w.Samples))
	}
}

func TestReadWAV_PCM8_Mono(t *testing.T) {
	// 8-bit WAV is unsigned: 0=min, 128=silence, 255=max.
	data := []byte{128, 0, 255}
	path := buildWAV(t, wavAudioFormatPCM, 1, 22050, 8, data)

	w, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if w.SampleRate != 22050 {
		t.Errorf("SampleRate = %d, want 22050", w.SampleRate)
	}
	if len(w.Samples) != 3 {
		t.Fatalf("len(Samples) = %d, want 3", len(w.Samples))
	}
	expected := []float32{0.0, -1.0, 0.992}
	for i, want := range expected {
		if math.Abs(float64(w.Samples[i]-want)) > 0.01 {
			t.Errorf("Samples[%d] = %g, want ~%g", i, w.Samples[i], want)
		}
	}
}

func TestReadWAV_Float32_Mono(t *testing.T) {
	input := []float32{0.0, 0.5, -0.5, 1.0}
	path := buildWAV(t, wavAudioFormatFloat, 1, 48000, 32, float32Bytes(input))

	w, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if len(w.Samples) != 4 {
		t.Fatalf("len(Samples) = %d, want 4", len(w.Samples))
	}
	for i, want := range input {
		// Float32 pass-through: exact equality.
		if w.Samples[i] != want {
			t.Errorf("Samples[%d] = %g, want %g (Float32 round-trips bit-identically)", i, w.Samples[i], want)
		}
	}
}

func TestReadWAV_FT8CaptureShape(t *testing.T) {
	// The canonical FT8 WAV from ft8sim / WSJT-X: 12 kHz mono PCM16.
	// A full 15-second slot is 180,000 samples; this test uses a
	// short stub to keep the temp file small while pinning the
	// shape that the FT8 pipeline expects.
	const ft8SampleRate = 12000
	input := []float32{0.0, 0.25, -0.25, 0.5, -0.5, 1.0, -1.0}
	path := buildWAV(t, wavAudioFormatPCM, 1, ft8SampleRate, 16, pcm16Bytes(input))

	w, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if w.SampleRate != ft8SampleRate {
		t.Errorf("SampleRate = %d, want %d (FT8 canonical)", w.SampleRate, ft8SampleRate)
	}
	if w.Channels != 1 {
		t.Errorf("Channels = %d, want 1", w.Channels)
	}
	if len(w.Samples) != len(input) {
		t.Errorf("len(Samples) = %d, want %d", len(w.Samples), len(input))
	}
}

func TestReadWAV_SkipsExtraChunks(t *testing.T) {
	path := buildWAVWithExtraChunk(t)
	w, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if w.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", w.SampleRate)
	}
	if len(w.Samples) != 2 {
		t.Errorf("len(Samples) = %d, want 2 (two PCM16 samples after the LIST chunk)", len(w.Samples))
	}
}

// --------------- ReadWAV — error cases ----------------------------------------

func TestReadWAV_FileNotFound(t *testing.T) {
	_, err := ReadWAV(filepath.Join(t.TempDir(), "nosuchfile.wav"))
	if err == nil {
		t.Error("ReadWAV(nonexistent) = nil err, want file-not-found error")
	}
}

func TestReadWAV_EmptyFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	_ = f.Close()

	_, err = ReadWAV(f.Name())
	if !errors.Is(err, ErrWAVInvalidHeader) {
		t.Errorf("ReadWAV(empty) err=%v, want ErrWAVInvalidHeader", err)
	}
}

func TestReadWAV_NotRIFF(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	if _, err := f.WriteString("OGG vorbis garbage data here!!!"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = f.Close()

	_, err = ReadWAV(f.Name())
	if !errors.Is(err, ErrWAVInvalidHeader) {
		t.Errorf("ReadWAV(non-RIFF) err=%v, want ErrWAVInvalidHeader", err)
	}
}

func TestReadWAV_RIFFButNotWAVE(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	if _, err := f.Write([]byte("RIFF")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(12)); err != nil {
		t.Fatalf("write size: %v", err)
	}
	if _, err := f.WriteString("AVI "); err != nil {
		t.Fatalf("write avi: %v", err)
	}
	_ = f.Close()

	_, err = ReadWAV(f.Name())
	if !errors.Is(err, ErrWAVInvalidHeader) {
		t.Errorf("ReadWAV(RIFF non-WAVE) err=%v, want ErrWAVInvalidHeader", err)
	}
}

func TestReadWAV_UnsupportedFormat_PCM24(t *testing.T) {
	data := make([]byte, 3) // one 24-bit sample's worth of bytes
	path := buildWAV(t, wavAudioFormatPCM, 1, 44100, 24, data)

	_, err := ReadWAV(path)
	if !errors.Is(err, ErrWAVUnsupportedFormat) {
		t.Errorf("ReadWAV(PCM24) err=%v, want ErrWAVUnsupportedFormat", err)
	}
}

func TestReadWAV_UnsupportedFormat_PCM32Int(t *testing.T) {
	// 32-bit integer PCM (audioFormat=1, bits=32) is distinct from
	// IEEE float 32-bit (audioFormat=3, bits=32) — only the latter
	// is supported.
	data := make([]byte, 4)
	path := buildWAV(t, wavAudioFormatPCM, 1, 44100, 32, data)

	_, err := ReadWAV(path)
	if !errors.Is(err, ErrWAVUnsupportedFormat) {
		t.Errorf("ReadWAV(PCM32-int) err=%v, want ErrWAVUnsupportedFormat", err)
	}
}

// --------------- convertWAVSamples direct tests ------------------------------

func TestConvertWAVSamples_PCM16_Silence(t *testing.T) {
	data := pcm16Bytes([]float32{0.0, 0.0})
	samples, err := convertWAVSamples(wavAudioFormatPCM, 16, data)
	if err != nil {
		t.Fatalf("convertWAVSamples: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("len = %d, want 2", len(samples))
	}
	if math.Abs(float64(samples[0])) > 0.001 {
		t.Errorf("silence sample = %g, want ~0", samples[0])
	}
}

func TestConvertWAVSamples_PCM16_OddBytes(t *testing.T) {
	_, err := convertWAVSamples(wavAudioFormatPCM, 16, []byte{0x00})
	if !errors.Is(err, ErrWAVInvalidHeader) {
		t.Errorf("PCM16 odd-byte err=%v, want ErrWAVInvalidHeader", err)
	}
}

func TestConvertWAVSamples_Float32_OddBytes(t *testing.T) {
	_, err := convertWAVSamples(wavAudioFormatFloat, 32, []byte{0x00, 0x01, 0x02})
	if !errors.Is(err, ErrWAVInvalidHeader) {
		t.Errorf("Float32 non-multiple-of-4 err=%v, want ErrWAVInvalidHeader", err)
	}
}

func TestConvertWAVSamples_PCM8_Range(t *testing.T) {
	samples, err := convertWAVSamples(wavAudioFormatPCM, 8, []byte{0, 128, 255})
	if err != nil {
		t.Fatalf("convertWAVSamples: %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("len = %d, want 3", len(samples))
	}
	if samples[0] > -0.9 {
		t.Errorf("byte 0 → %g, want < -0.9 (PCM8 minimum)", samples[0])
	}
	if math.Abs(float64(samples[1])) > 0.01 {
		t.Errorf("byte 128 → %g, want ~0 (PCM8 silence)", samples[1])
	}
	if samples[2] < 0.9 {
		t.Errorf("byte 255 → %g, want > 0.9 (PCM8 near-max)", samples[2])
	}
}

// --------------- WriteWAV ------------------------------------------------------

// TestWriteWAV_RoundTrip pins WriteWAV as the inverse of ReadWAV's 16-bit
// branch: a float32 buffer written then read back matches within one int16
// LSB, with the header fields and file size preserved.
func TestWriteWAV_RoundTrip(t *testing.T) {
	in := &Data{SampleRate: 12000, Channels: 1, Samples: make([]float32, 4096)}
	for i := range in.Samples {
		in.Samples[i] = float32(0.8 * math.Sin(2*math.Pi*1500*float64(i)/12000))
	}
	// Endpoints exercise the clamp branches (+1.0 must hit the int16 ceiling
	// rather than wrapping to -32768).
	in.Samples[0], in.Samples[1] = 1.0, -1.0

	path := filepath.Join(t.TempDir(), "rt.wav")
	if err := WriteWAV(path, in); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}

	out, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if out.SampleRate != 12000 || out.Channels != 1 || len(out.Samples) != len(in.Samples) {
		t.Fatalf("header/len mismatch: rate=%d ch=%d n=%d", out.SampleRate, out.Channels, len(out.Samples))
	}

	const oneLSB = 1.0/32767.0 + 1e-6
	for i := range in.Samples {
		if d := math.Abs(float64(in.Samples[i] - out.Samples[i])); d > oneLSB {
			t.Fatalf("sample %d: round-trip error %g > 1 LSB", i, d)
		}
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if want := int64(44 + len(in.Samples)*2); st.Size() != want {
		t.Errorf("file size = %d, want %d (44-byte header + 2 bytes/sample)", st.Size(), want)
	}
}

func TestWriteWAV_NilData(t *testing.T) {
	if err := WriteWAV(filepath.Join(t.TempDir(), "nil.wav"), nil); err == nil {
		t.Error("WriteWAV(nil) = nil err, want error")
	}
}

// TestWriteWAV_DefaultsMonoOnZeroChannels guards the convenience default:
// a Data with Channels==0 (e.g. hand-built from a capture buffer) writes
// as mono rather than producing a zero-blockAlign header.
func TestWriteWAV_DefaultsMonoOnZeroChannels(t *testing.T) {
	in := &Data{SampleRate: 12000, Channels: 0, Samples: []float32{0, 0.5, -0.5}}
	path := filepath.Join(t.TempDir(), "mono.wav")
	if err := WriteWAV(path, in); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}
	out, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if out.Channels != 1 {
		t.Errorf("Channels = %d, want 1 (defaulted)", out.Channels)
	}
}

// --------------- ReadWAV — chunk-alignment + error-sentinel regressions --------

// wavBytes assembles raw WAV bytes from mixed string (chunk IDs / magic) and
// fixed-size LE values — for hand-crafted edge cases the structured builders can't
// express (odd-sized chunks, truncated/misordered headers).
func wavBytes(parts ...any) []byte {
	var buf bytes.Buffer
	for _, p := range parts {
		if s, ok := p.(string); ok {
			buf.WriteString(s)
			continue
		}
		if err := binary.Write(&buf, binary.LittleEndian, p); err != nil {
			panic(err) // test-only: a wrong part type is a test bug
		}
	}
	return buf.Bytes()
}

// writeRawWAV writes raw bytes to a temp .wav and returns the path.
func writeRawWAV(t *testing.T, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "raw.wav")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write raw wav: %v", err)
	}
	return path
}

// An odd-sized fmt chunk is followed by a RIFF word-alignment pad byte; ReadWAV must
// consume it so the next chunk ID (data) reads on a word boundary. Regression for the
// fmt-branch pad bug — the unknown-chunk (default) branch already padded, but fmt did
// not, so an odd fmt size shifted every following chunk by one byte.
func TestReadWAV_OddSizedFmtChunkPad(t *testing.T) {
	pcm := []byte{0x00, 0x00, 0xFF, 0x7F} // two PCM16 samples (silence, max+)
	raw := wavBytes(
		"RIFF", uint32(42), "WAVE",
		"fmt ", uint32(17), // 16 standard bytes + 1 extension byte = odd
		uint16(1), uint16(1), uint32(12000), uint32(24000), uint16(2), uint16(16),
		uint8(0xAB), // the 1 extra fmt byte (makes chunkSize 17)
		uint8(0x00), // RIFF pad byte word-aligning the odd chunk
		"data", uint32(len(pcm)), pcm,
	)
	w, err := ReadWAV(writeRawWAV(t, raw))
	if err != nil {
		t.Fatalf("ReadWAV(odd-fmt): %v", err)
	}
	if w.SampleRate != 12000 || w.Channels != 1 || len(w.Samples) != 2 {
		t.Fatalf("odd-fmt parse = rate %d / ch %d / n %d, want 12000 / 1 / 2",
			w.SampleRate, w.Channels, len(w.Samples))
	}
}

// A file truncated inside the fmt chunk must classify as ErrWAVInvalidHeader, not a
// raw I/O error — so callers can branch on errors.Is. Regression for sentinel
// preservation in the fmt-read path.
func TestReadWAV_TruncatedFmt_IsInvalidHeader(t *testing.T) {
	// fmt declares 16 bytes but the file ends after 2 of them.
	raw := wavBytes("RIFF", uint32(20), "WAVE", "fmt ", uint32(16), uint16(1))
	_, err := ReadWAV(writeRawWAV(t, raw))
	if !errors.Is(err, ErrWAVInvalidHeader) {
		t.Errorf("ReadWAV(truncated fmt) err=%v, want ErrWAVInvalidHeader", err)
	}
}

// A data chunk before any fmt chunk must classify as ErrWAVInvalidHeader (was a
// message-only error). Regression for sentinel preservation in the data path.
func TestReadWAV_DataBeforeFmt_IsInvalidHeader(t *testing.T) {
	pcm := []byte{0x00, 0x00}
	raw := wavBytes("RIFF", uint32(24), "WAVE", "data", uint32(len(pcm)), pcm)
	_, err := ReadWAV(writeRawWAV(t, raw))
	if !errors.Is(err, ErrWAVInvalidHeader) {
		t.Errorf("ReadWAV(data-before-fmt) err=%v, want ErrWAVInvalidHeader", err)
	}
}
