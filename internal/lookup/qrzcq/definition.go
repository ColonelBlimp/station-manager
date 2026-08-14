package qrzcq

import "encoding/xml"

// Callsign mirrors QRZCQ's XML <Callsign> element. The XML interface is mostly
// QRZ-compatible, but its field names are not: for example locator rather than
// grid, latitude/longitude rather than lat/lon, and qth/address/city rather
// than QRZ.com's addr1/addr2 split.
type Callsign struct {
	Call      string `xml:"call"`
	Name      string `xml:"name"`
	QTH       string `xml:"qth"`
	Address   string `xml:"address"`
	City      string `xml:"city"`
	Zip       string `xml:"zip"`
	License   string `xml:"license"`
	Continent string `xml:"continent"`
	Country   string `xml:"country"`
	State     string `xml:"state"`
	County    string `xml:"county"`
	BManager  string `xml:"bmanager"`
	Manager   string `xml:"manager"`
	Locator   string `xml:"locator"`
	Latitude  string `xml:"latitude"`
	Longitude string `xml:"longitude"`
	Website   string `xml:"website"`
	DXCC      string `xml:"dxcc"`
	ITU       string `xml:"itu"`
	CQ        string `xml:"cq"`
	IOTA      string `xml:"iota"`
	Plot      string `xml:"plot"`
	DOK       string `xml:"dok"`
	SonDOK    string `xml:"sondok"`
	EQSL      string `xml:"eqsl"`
	LOTW      string `xml:"lotw"`
	BQSL      string `xml:"bqsl"`
	MQSL      string `xml:"mqsl"`
	UTF8      string `xml:"utf8"`
	QSLPic    string `xml:"qslpic"`
	Prefix    string `xml:"prefix"`
}

// Session carries both login results and per-lookup status. QRZCQ documents
// session keys as valid for three days, but callers must still react to the
// server's Error field because a key may be invalidated sooner.
type Session struct {
	Key     string `xml:"Key"`
	Count   int    `xml:"Count"`
	SubExp  string `xml:"SubExp"`
	GMTime  string `xml:"GMTime"`
	Remark  string `xml:"Remark"`
	Message string `xml:"Message"`
	Error   string `xml:"Error"`
}

// Database is QRZCQ's XML response envelope. encoding/xml matches child tags
// by local name, so the documented default xmlns="http://qrzcq.com" is
// accepted without coupling the parser to namespace-prefix spelling.
type Database struct {
	XMLName  xml.Name `xml:"QRZCQDatabase"`
	Version  string   `xml:"version,attr"`
	Callsign Callsign `xml:"Callsign"`
	Session  Session  `xml:"Session"`
}
