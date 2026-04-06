// Package codec implements the LDPC(174,91) encoder and decoder used by FT8
// and FT4 digital modes.
//
// The FT8 forward-error-correction scheme uses a (174,91) irregular LDPC code
// with variable-node degree 3. Every coded message consists of 91 information
// bits (77 message + 14 CRC) and 83 parity bits, for a total of 174 coded
// bits.
//
// This package provides:
//
//   - Matrix constants ([G], [Mn], [Nm], [NmCount]) defining the code's
//     generator and parity-check structure.
//   - [Encode] — systematic LDPC encoder (TX path).
//   - [Decode] — normalised min-sum belief-propagation decoder (RX path).
//   - (Future) [EncodeMessage] / [DecodeMessage] — convenience wrappers bridging
//     the [message] package with the raw LDPC functions.
//
// The encode chain is: message.Pack → message.Append91 → codec.Encode →
// symbol mapping → GFSK synthesis.
//
// The decode chain is: soft demodulation → codec.Decode → CRC-14 verify →
// message.Unpack.
//
// Matrix data provenance:
//
//   - Generator matrix G: WSJT-X lib/ft8/LDPC_174_91_3_generator.f90
//   - Tanner graph arrays Mn, Nm, NmCount: ft8_lib (github.com/kgoba/ft8_lib)
//     constants.c / constants.h
//
// Reference: ft8_lib (https://github.com/kgoba/ft8_lib),
// WSJT-X (https://sourceforge.net/projects/wsjt/).
package codec
