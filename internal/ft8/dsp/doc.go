// Package dsp implements the FT8 DSP pipeline: symbol mapping, FFT-based
// spectrogram computation, candidate detection, and soft demodulation.
//
// This package bridges the bit-level LDPC codec ([codec]) with the audio
// domain. On the TX side it maps coded bits to 8-FSK channel symbols; on
// the RX side it processes captured audio to produce soft LLR inputs for
// the LDPC decoder.
//
// The symbol mapping utilities ([BitsToSymbols], [InsertSync], and their
// inverses) are the foundation: they are needed by both the TX synthesis
// path and the RX demodulator.
//
// FT8 protocol parameters (8-FSK, 6.25 baud, 6.25 Hz tone spacing,
// 79 symbols per message) are defined as package-level constants.
//
// Reference: ft8_lib (https://github.com/kgoba/ft8_lib),
// WSJT-X (https://sourceforge.net/projects/wsjt/).
package dsp
