package qrz

import "encoding/xml"

// Callsign mirrors the QRZ.com XML <Callsign> element. Field-name
// fidelity to the upstream API matters here — the XML tag values are
// case-sensitive on the wire, and the JSON tags are kept for
// historical compatibility with operator-typed `additional_data`
// blobs that may have been written when v1 used these names.
//
// Country / Cqzone / Ituzone / Dxcc are populated by QRZ but per
// ADR 0017 #2 the orchestrator's FilterToCallsignFields strips them
// at the merge boundary — they're hamnut-exclusive on write. Keeping
// the fields here lets the upstream parse stay faithful to the wire
// shape; the filter at the orchestrator boundary is the single
// enforcement point.
type Callsign struct {
	Call      string `xml:"call" json:"call"`
	Aliases   string `xml:"aliases"`
	Dxcc      string `xml:"dxcc"`
	Fname     string `xml:"fname"`
	Name      string `xml:"name"`
	Addr1     string `xml:"addr1"`
	Addr2     string `xml:"addr2" json:"qth"`
	State     string `xml:"state"`
	Zip       string `xml:"zip"`
	Country   string `xml:"country" json:"country"`
	Ccode     string `xml:"ccode"`
	Lat       string `xml:"lat" json:"lat"`
	Lon       string `xml:"lon" json:"lon"`
	Grid      string `xml:"grid" json:"gridsquare"`
	County    string `xml:"county"`
	Fips      string `xml:"fips"`
	Land      string `xml:"land"`
	Efdate    string `xml:"efdate"`
	Expdate   string `xml:"expdate"`
	PCall     string `xml:"p_call"`
	Class     string `xml:"class"`
	Codes     string `xml:"codes"`
	Qslmgr    string `xml:"qslmgr"`
	Email     string `xml:"email" json:"email"`
	URL       string `xml:"url"`
	UViews    int    `xml:"u_views"`
	Bio       string `xml:"bio"`
	Image     string `xml:"image"`
	Serial    int    `xml:"serial"`
	Moddate   string `xml:"moddate"`
	MSA       int    `xml:"MSA"`
	AreaCode  string `xml:"AreaCode"`
	TimeZone  string `xml:"TimeZone"`
	GMTOffset string `xml:"GMTOffset"`
	DST       string `xml:"DST"`
	Eqsl      string `xml:"eqsl"`
	Mqsl      string `xml:"mqsl"`
	Cqzone    string `xml:"cqzone" json:"cqz"`
	Ituzone   string `xml:"ituzone" json:"ituz"`
	Geoloc    string `xml:"geoloc"`
	Attn      string `xml:"attn"`
	Nickname  string `xml:"nickname" json:"name"`
	NameFmt   string `xml:"name_fmt"`
	Born      string `xml:"born"`
}

// Session is the auth/session block in QRZ's XML response. Key is the
// session token used for subsequent calls; Error carries upstream
// errors including "Not found" when a callsign isn't in the DB.
type Session struct {
	Key    string `xml:"Key"`
	Count  int    `xml:"Count"`
	SubExp string `xml:"SubExp"`
	GMTime string `xml:"GMTime"`
	Remark string `xml:"Remark"`
	Error  string `xml:"Error"`
}

// Database is the root element of QRZ's XML responses — wraps both
// session metadata and the queried callsign record.
type Database struct {
	XMLName  xml.Name `xml:"QRZDatabase"`
	Version  string   `xml:"version,attr"`
	Callsign Callsign `xml:"Callsign"`
	Session  Session  `xml:"Session"`
}
