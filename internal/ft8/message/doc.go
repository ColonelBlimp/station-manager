// Package message implements FT8/FT4 77-bit message packing and unpacking.
//
// FT8 messages are 77 bits packed MSB-first into 10 bytes. The last 3 bits
// (positions 74–76) encode the i3 field that identifies the message type.
// For i3=0, the preceding 3 bits (positions 71–73) encode the n3 sub-type.
//
// Supported message types:
//
//   - Type 1 (i3=1): Standard QSO exchange — two callsigns + grid/report/token.
//     Covers ~90% of on-air FT8 traffic (CQ, signal report, Roger, RR73/73).
//   - Type 4 (i3=4): Non-standard callsign — one callsign encoded in 58 bits
//     (base-38, up to 11 chars including '/') + one 12-bit hashed callsign.
//     Handles compound calls like VK/ZL4XZ, PJ4/KA1ABC.
//   - Type 0 (i3=0, n3=0): Free-text — up to 13 characters from a 42-symbol
//     alphabet, base-42 encoded into 71 bits.
//
// Entry points:
//
//   - [Pack] encodes a [Message] into a 10-byte payload.
//   - [Unpack] decodes a 10-byte payload into a [Message].
//   - [CRC14] computes the 14-bit CRC over a 77-bit message.
//   - [Append91] produces the 91-bit payload (77 message + 14 CRC) for LDPC encoding.
//
// Field-level encode/decode functions ([EncodeCallsign], [DecodeCallsign],
// [EncodeGrid], [DecodeGridField], [EncodeFreeText], [DecodeFreeText]) are
// also exported for direct use.
//
// Reference: ft8_lib (https://github.com/kgoba/ft8_lib) commit 9fec6ca,
// WSJT-X lib/ft8/pack77.f90.
package message
