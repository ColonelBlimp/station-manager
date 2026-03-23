package qrz

import (
	"encoding/xml"
)

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

type Session struct {
	Key    string `xml:"Key"`
	Count  int    `xml:"Count"`
	SubExp string `xml:"SubExp"`
	GMTime string `xml:"GMTime"`
	Remark string `xml:"Remark"`
	Error  string `xml:"Error"`
}

type Database struct {
	XMLName  xml.Name `xml:"QRZDatabase"`
	Version  string   `xml:"version,attr"`
	Callsign Callsign `xml:"Callsign"`
	Session  Session  `xml:"Session"`
}
