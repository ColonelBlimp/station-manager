package audio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildWAV writes a minimal RIFF/WAVE file to a temp file and returns its path.
// audioFormat: 1=PCM, 3=IEEE float
// bitsPerSample: 8, 16, or 32
// data: raw PCM bytes
func buildWAV(t *testing.T, audioFormat, channels uint16, sampleRate uint32, bitsPerSample uint16, data []byte) string {
	t.Helper()

	fmtChunkSize := uint32(16)
	dataChunkSize := uint32(len(data))
	fileSize := 4 + 8 + fmtChunkSize + 8 + dataChunkSize // "WAVE" + fmt chunk + data chunk

	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })

	write := func(v any) {
		require.NoError(t, binary.Write(f, binary.LittleEndian, v))
	}
	writeStr := func(s string) {
		_, err := f.WriteString(s)
		require.NoError(t, err)
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
	_, err = f.Write(data)
	require.NoError(t, err)

	return f.Name()
}

// buildWAVWithExtraChunk inserts an unknown chunk before "data" to test chunk skipping.
func buildWAVWithExtraChunk(t *testing.T) string {
	t.Helper()
	// Two PCM16 samples: 0x0000 (silence), 0x7FFF (max positive)
	pcm := []byte{0x00, 0x00, 0xFF, 0x7F}

	extra := []byte("LIST\x04\x00\x00\x00INFO") // 8-byte header + 4 bytes payload (word-aligned)
	fmtSize := uint32(16)
	extraSize := uint32(len(extra))
	dataSize := uint32(len(pcm))
	fileSize := 4 + 8 + fmtSize + extraSize + 8 + dataSize

	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })

	write := func(v any) {
		require.NoError(t, binary.Write(f, binary.LittleEndian, v))
	}
	writeStr := func(s string) {
		_, err := f.WriteString(s)
		require.NoError(t, err)
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

	// Extra unknown chunk (LIST with 4-byte payload — already word-aligned)
	writeStr("LIST")
	write(uint32(4))
	writeStr("INFO")

	writeStr("data")
	write(dataSize)
	_, err = f.Write(pcm)
	require.NoError(t, err)

	return f.Name()
}

// pcm16Bytes encodes float32 values ([-1, 1]) as little-endian int16 PCM.
func pcm16Bytes(samples []float32) []byte {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		v := int16(s * 32767)
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}
	return buf
}

// float32Bytes encodes float32 values as little-endian IEEE 754 bytes.
func float32Bytes(samples []float32) []byte {
	buf := make([]byte, len(samples)*4)
	for i, s := range samples {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(s))
	}
	return buf
}

// --------------- readWAV — valid files ----------------------------------------

func TestReadWAV_PCM16_Mono(t *testing.T) {
	input := []float32{0.0, 1.0, -1.0, 0.5}
	path := buildWAV(t, wavAudioFormatPCM, 1, 44100, 16, pcm16Bytes(input))

	w, err := readWAV(path)
	require.NoError(t, err)
	require.Equal(t, uint32(44100), w.SampleRate)
	require.Equal(t, uint16(1), w.Channels)
	require.Len(t, w.Samples, 4)
	require.InDelta(t, 0.0, w.Samples[0], 0.0001)
	require.InDelta(t, 1.0, w.Samples[1], 0.0001)
	require.InDelta(t, -1.0, w.Samples[2], 0.0001)
	require.InDelta(t, 0.5, w.Samples[3], 0.001)
}

func TestReadWAV_PCM16_Stereo(t *testing.T) {
	// 2 stereo frames = 4 interleaved samples
	input := []float32{0.1, 0.2, 0.3, 0.4}
	path := buildWAV(t, wavAudioFormatPCM, 2, 48000, 16, pcm16Bytes(input))

	w, err := readWAV(path)
	require.NoError(t, err)
	require.Equal(t, uint16(2), w.Channels)
	require.Len(t, w.Samples, 4)
}

func TestReadWAV_PCM8_Mono(t *testing.T) {
	// 8-bit WAV: 128=silence, 0=min, 255=max
	data := []byte{128, 0, 255}
	path := buildWAV(t, wavAudioFormatPCM, 1, 22050, 8, data)

	w, err := readWAV(path)
	require.NoError(t, err)
	require.Equal(t, uint32(22050), w.SampleRate)
	require.Len(t, w.Samples, 3)
	require.InDelta(t, 0.0, w.Samples[0], 0.01)   // 128 → silence
	require.InDelta(t, -1.0, w.Samples[1], 0.01)  // 0 → min
	require.InDelta(t, 0.992, w.Samples[2], 0.01) // 255 → ~max
}

func TestReadWAV_Float32_Mono(t *testing.T) {
	input := []float32{0.0, 0.5, -0.5, 1.0}
	path := buildWAV(t, wavAudioFormatFloat, 1, 48000, 32, float32Bytes(input))

	w, err := readWAV(path)
	require.NoError(t, err)
	require.Len(t, w.Samples, 4)
	require.Equal(t, input, w.Samples)
}

func TestReadWAV_48kHz_SampleRate(t *testing.T) {
	path := buildWAV(t, wavAudioFormatPCM, 1, 48000, 16, pcm16Bytes([]float32{0}))
	w, err := readWAV(path)
	require.NoError(t, err)
	require.Equal(t, uint32(48000), w.SampleRate)
}

func TestReadWAV_SkipsExtraChunks(t *testing.T) {
	path := buildWAVWithExtraChunk(t)
	w, err := readWAV(path)
	require.NoError(t, err)
	require.Equal(t, uint32(44100), w.SampleRate)
	require.Len(t, w.Samples, 2) // two PCM16 samples in the data chunk
}

// --------------- readWAV — error cases ----------------------------------------

func TestReadWAV_FileNotFound(t *testing.T) {
	_, err := readWAV(filepath.Join(t.TempDir(), "nosuchfile.wav"))
	require.Error(t, err)
}

func TestReadWAV_EmptyFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	require.NoError(t, err)
	f.Close()

	_, err = readWAV(f.Name())
	require.ErrorIs(t, err, ErrWAVInvalidHeader)
}

func TestReadWAV_NotRIFF(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	require.NoError(t, err)
	_, err = f.WriteString("OGG vorbis garbage data here!!!")
	require.NoError(t, err)
	f.Close()

	_, err = readWAV(f.Name())
	require.ErrorIs(t, err, ErrWAVInvalidHeader)
}

func TestReadWAV_RIFFButNotWAVE(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.wav")
	require.NoError(t, err)
	require.NoError(t, binary.Write(f, binary.LittleEndian, []byte("RIFF")))
	require.NoError(t, binary.Write(f, binary.LittleEndian, uint32(12)))
	_, err = f.WriteString("AVI ") // not WAVE
	require.NoError(t, err)
	f.Close()

	_, err = readWAV(f.Name())
	require.ErrorIs(t, err, ErrWAVInvalidHeader)
}

func TestReadWAV_UnsupportedFormat_PCM24(t *testing.T) {
	// 24-bit PCM is not supported
	data := make([]byte, 3) // one 24-bit sample
	path := buildWAV(t, wavAudioFormatPCM, 1, 44100, 24, data)

	_, err := readWAV(path)
	require.ErrorIs(t, err, ErrWAVUnsupportedFormat)
}

func TestReadWAV_UnsupportedFormat_PCM32Int(t *testing.T) {
	// 32-bit integer PCM (audioFormat=1, bits=32) is different from float32 (audioFormat=3)
	data := make([]byte, 4)
	path := buildWAV(t, wavAudioFormatPCM, 1, 44100, 32, data)

	_, err := readWAV(path)
	require.ErrorIs(t, err, ErrWAVUnsupportedFormat)
}

// --------------- convertWAVSamples -------------------------------------------

func TestConvertWAVSamples_PCM16_Silence(t *testing.T) {
	data := pcm16Bytes([]float32{0.0, 0.0})
	samples, err := convertWAVSamples(wavAudioFormatPCM, 16, data)
	require.NoError(t, err)
	require.Len(t, samples, 2)
	require.InDelta(t, 0.0, samples[0], 0.0001)
}

func TestConvertWAVSamples_PCM16_OddBytes(t *testing.T) {
	_, err := convertWAVSamples(wavAudioFormatPCM, 16, []byte{0x00}) // odd length
	require.ErrorIs(t, err, ErrWAVInvalidHeader)
}

func TestConvertWAVSamples_Float32_OddBytes(t *testing.T) {
	_, err := convertWAVSamples(wavAudioFormatFloat, 32, []byte{0x00, 0x01, 0x02}) // not multiple of 4
	require.ErrorIs(t, err, ErrWAVInvalidHeader)
}

func TestConvertWAVSamples_PCM8_Range(t *testing.T) {
	data := []byte{0, 128, 255}
	samples, err := convertWAVSamples(wavAudioFormatPCM, 8, data)
	require.NoError(t, err)
	require.Len(t, samples, 3)
	require.True(t, samples[0] < -0.9)        // 0 = min
	require.InDelta(t, 0.0, samples[1], 0.01) // 128 = silence
	require.True(t, samples[2] > 0.9)         // 255 = near max
}
