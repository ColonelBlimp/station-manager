package types

type ContactHistory struct {
	ID      int64  `json:"id"`
	Band    string `json:"band"`
	Freq    string `json:"freq" boil:"freq"`
	Mode    string `json:"mode"`
	QsoDate string `json:"qso_date" boil:"qso_date"`
	TimeOn  string `json:"time_on"`
	Name    string `json:"name"`
	Country string `json:"country"`
	Call    string `json:"call"`
	RstSent string `json:"rst_sent"`
	RstRcvd string `json:"rst_rcvd"`
	Notes   string `json:"notes"`
}
