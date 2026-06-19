// Package pskreporter uploads FT8 reception reports ("spots") to PSK Reporter
// (https://pskreporter.info/pskdev.html) as IPFIX (RFC 5101) UDP datagrams.
//
// This file is the wire-format encoder; service.go owns the spot buffer, dedup,
// flush cadence, and UDP transport. Encoding is deliberately split out so it can
// be table-tested byte-for-byte against the spec's worked example with no network.
//
// All multi-byte values are network order (big-endian). Enterprise-specific
// attributes carry enterprise number 30351 (registered to the PSK Reporter
// author); standard IPFIX fields (flowStartSeconds = 150) carry none.
package pskreporter

import "encoding/binary"

const (
	ipfixVersion = 0x000A // IPFIX version, the first 2 bytes of every datagram

	enterpriseNumber = 30351 // 0x0000_768F — PSK Reporter's IANA enterprise number

	// Template (descriptor) link ids tie a descriptor to its data block. Arbitrary
	// values in [256, 65535]; these match the spec's examples.
	rxTemplateID = 0x9992 // receiver information record
	txTemplateID = 0x9993 // sender information records

	setIDTemplate        = 0x0002 // a plain template set (the sender descriptor)
	setIDOptionsTemplate = 0x0003 // an options template set (the receiver descriptor; carries a scope field count)

	// informationSource = 1 ("automatically extracted"). The server ignores any
	// record without this; 2 would mark it as a QSO result.
	infoSourceAutomatic = 1
)

// IPFIX field ids (low byte; enterprise 30351 unless std). From the attribute
// table at pskreporter.info/pskdev.html.
const (
	fidSenderCallsign    = 0x01
	fidReceiverCallsign  = 0x02
	fidSenderLocator     = 0x03
	fidReceiverLocator   = 0x04
	fidFrequency         = 0x05
	fidSNR               = 0x06
	fidIMD               = 0x07
	fidDecoderSoftware   = 0x08
	fidAntenna           = 0x09
	fidMode              = 0x0A
	fidInformationSource = 0x0B
	fidFlowStartSeconds  = 150 // standard IPFIX field — no enterprise number
)

const varLength = 0xFFFF // field length sentinel for a variable-length string

// Spot is one reception report: a station we decoded and the conditions.
type Spot struct {
	Call     string // sender callsign (the transmitting station)
	Grid     string // sender locator; "" when unknown (sent as an empty string)
	FreqHz   uint32 // absolute frequency in Hz (dial + audio offset)
	SNR      int8   // signal-to-noise ratio, dB
	Mode     string // ADIF MODE, e.g. "FT8"
	TimeUnix uint32 // transmission time, unix seconds (flowStartSeconds)
}

// Receiver is the single receiver-info record sent in every datagram: us.
type Receiver struct {
	Call     string
	Locator  string
	Software string // decoderSoftware, e.g. "StationManager 2.0.0"
	Antenna  string // antennaInformation; "" is sent as an empty variable-length field (the encoder always uses the fixed 4-field receiver template)
}

// tField is one field in a template descriptor.
type tField struct {
	id     uint16
	length uint16 // varLength for a variable-length string, else the fixed byte width
	std    bool   // standard IPFIX field (no enterprise number) vs enterprise 30351
}

func (f tField) appendTo(b []byte) []byte {
	if f.std {
		b = binary.BigEndian.AppendUint16(b, f.id)
		return binary.BigEndian.AppendUint16(b, f.length)
	}
	b = binary.BigEndian.AppendUint16(b, 0x8000|f.id) // 0x8000 = enterprise-specific flag
	b = binary.BigEndian.AppendUint16(b, f.length)
	return binary.BigEndian.AppendUint32(b, enterpriseNumber)
}

// senderFields is the count-8 sender template: the richest the spec offers, so a
// single template covers every spot. iMD is always 0 (FT8 carries no IMD) and the
// locator is an empty string when unknown — both cheaper than juggling templates.
var senderFields = []tField{
	{id: fidSenderCallsign, length: varLength},
	{id: fidFrequency, length: 4},
	{id: fidSNR, length: 1},
	{id: fidIMD, length: 1},
	{id: fidMode, length: varLength},
	{id: fidInformationSource, length: 1},
	{id: fidSenderLocator, length: varLength},
	{id: fidFlowStartSeconds, length: 4, std: true},
}

// receiverFields returns the receiver template. antennaInformation is ALWAYS one of
// the four fields (sent as an empty string when unset), so the template shape is
// invariant — the field count never changes with config. Descriptors ride only the
// first few datagrams + hourly, so a fixed shape is what lets a runtime SetReceiver
// (antenna added/removed) stay consistent with the cached template ID. Mirrors the
// always-richest sender template.
func receiverFields() []tField {
	return []tField{
		{id: fidReceiverCallsign, length: varLength},
		{id: fidReceiverLocator, length: varLength},
		{id: fidDecoderSoftware, length: varLength},
		{id: fidAntenna, length: varLength},
	}
}

// encodeTemplate builds a descriptor set. optionsScope > 0 → an options template
// set (the receiver descriptor) carrying a scope field count; 0 → a plain template
// set (the sender descriptor). The set is null-padded to a multiple of 4 bytes.
func encodeTemplate(templateID, optionsScope uint16, fields []tField) []byte {
	var inner []byte
	inner = binary.BigEndian.AppendUint16(inner, templateID)
	inner = binary.BigEndian.AppendUint16(inner, uint16(len(fields)))
	if optionsScope > 0 {
		inner = binary.BigEndian.AppendUint16(inner, optionsScope)
	}
	for _, f := range fields {
		inner = f.appendTo(inner)
	}
	setID := uint16(setIDTemplate)
	if optionsScope > 0 {
		setID = setIDOptionsTemplate
	}
	return wrapSet(setID, inner)
}

// wrapSet prefixes a set's payload with its (setID, setLength) header and pads the
// whole set to a multiple of 4. setLength counts the header + payload + padding.
func wrapSet(setID uint16, payload []byte) []byte {
	total := 4 + len(payload)
	pad := (4 - total%4) % 4
	total += pad
	out := binary.BigEndian.AppendUint16(nil, setID)
	out = binary.BigEndian.AppendUint16(out, uint16(total))
	out = append(out, payload...)
	return append(out, make([]byte, pad)...)
}

// appendVarStr appends a length-prefixed (1 byte) UTF-8 string. The length code
// excludes itself; the spec caps a field at 254 bytes, so longer strings truncate.
func appendVarStr(b []byte, s string) []byte {
	if len(s) > 254 {
		s = s[:254]
	}
	b = append(b, byte(len(s)))
	return append(b, s...)
}

// encodeReceiverRecord builds the receiver-info data block (link id 0x9992). The
// four fields always match the fixed 4-field receiver template; antenna is sent as
// an empty string when unset (a length-0 field — for the no-antenna case this byte
// coincides with what would otherwise be padding, so the bytes are unchanged).
func encodeReceiverRecord(r Receiver) []byte {
	var rec []byte
	rec = appendVarStr(rec, r.Call)
	rec = appendVarStr(rec, r.Locator)
	rec = appendVarStr(rec, r.Software)
	rec = appendVarStr(rec, r.Antenna)
	return wrapSet(rxTemplateID, rec)
}

// encodeSenderRecords builds the sender-info data block (link id 0x9993): one
// count-8 record per spot, the whole block padded to a multiple of 4. There is no
// padding between records.
func encodeSenderRecords(spots []Spot) []byte {
	var recs []byte
	for _, s := range spots {
		recs = appendVarStr(recs, s.Call)
		recs = binary.BigEndian.AppendUint32(recs, s.FreqHz)
		recs = append(recs, byte(s.SNR)) // int8 → one byte
		recs = append(recs, 0)           // iMD: FT8 carries none
		recs = appendVarStr(recs, s.Mode)
		recs = append(recs, infoSourceAutomatic)
		recs = appendVarStr(recs, s.Grid) // empty string when unknown
		recs = binary.BigEndian.AppendUint32(recs, s.TimeUnix)
	}
	return wrapSet(txTemplateID, recs)
}

// encodeDatagram assembles a full PSK Reporter datagram: the 16-byte header, the
// two descriptors (when includeTemplates — the first few packets + hourly), the
// receiver record, and the sender records. seq is the cumulative report count (per
// spec: not the packet count); id is the constant per-session random identifier.
func encodeDatagram(timeUnix, seq, id uint32, includeTemplates bool, r Receiver, spots []Spot) []byte {
	var body []byte
	if includeTemplates {
		body = append(body, encodeTemplate(rxTemplateID, 1, receiverFields())...)
		body = append(body, encodeTemplate(txTemplateID, 0, senderFields)...)
	}
	body = append(body, encodeReceiverRecord(r)...)
	body = append(body, encodeSenderRecords(spots)...)

	out := binary.BigEndian.AppendUint16(nil, ipfixVersion)
	out = binary.BigEndian.AppendUint16(out, uint16(16+len(body)))
	out = binary.BigEndian.AppendUint32(out, timeUnix)
	out = binary.BigEndian.AppendUint32(out, seq)
	out = binary.BigEndian.AppendUint32(out, id)
	return append(out, body...)
}
