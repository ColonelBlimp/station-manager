package qrz

import (
	stderrors "errors"
	smerrors "github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"testing"
)

func TestService_unmarshalResponse(t *testing.T) {
	service := &Service{}

	successXML := `<?xml version="1.0"?>
<QRZDatabase version="1.34">
  <Callsign>
    <call>aa7bq</call>
    <fname>FRED L</fname>
    <name>LLOYD</name>
    <addr1>8711 E PINNACLE PEAK RD 193</addr1>
    <addr2>SCOTTSDALE</addr2>
    <state>AZ</state>
    <zip>85255</zip>
    <country>United States</country>
    <grid>DM32af</grid>
    <cqzone>3</cqzone>
    <ituzone>2</ituzone>
    <dxcc>291</dxcc>
    <email>flloyd@qrz.com</email>
    <p_call>KJ6RK</p_call>
    <url>https://www.qrz.com/db/aa7bq</url>
    <lat>34.23456</lat>
    <lon>-112.34356</lon>
    <attn>c/o QRZ LLC</attn>
    <name_fmt>FRED "The Boss" LLOYD</name_fmt>
  </Callsign>
  <Session></Session>
</QRZDatabase>`

	sessionErrorXML := `<?xml version="1.0"?>
<QRZDatabase version="1.34">
  <Callsign></Callsign>
  <Session><Error>Invalid session key</Error></Session>
</QRZDatabase>`

	notFoundXML := `<?xml version="1.0"?>
<QRZDatabase version="1.34">
  <Callsign></Callsign>
  <Session><Error>Callsign not found</Error></Session>
</QRZDatabase>`

	missingCallXML := `<?xml version="1.0"?>
<QRZDatabase version="1.34">
  <Callsign>
    <name>LLOYD</name>
  </Callsign>
  <Session></Session>
</QRZDatabase>`

	tests := []struct {
		name          string
		payload       string
		want          types.ContactedStation
		wantErr       bool
		wantNotFound  bool
		wantErrString string
	}{
		{
			name:    "successfully maps contact",
			payload: successXML,
			want: types.ContactedStation{
				Call:        "AA7BQ",
				Name:        "FRED \"The Boss\" LLOYD",
				Address:     "8711 E PINNACLE PEAK RD 193, SCOTTSDALE, AZ, 85255, United States",
				QTH:         "SCOTTSDALE, AZ",
				Country:     "United States",
				Gridsquare:  "DM32AF",
				CQZ:         "3",
				ITUZ:        "2",
				DXCC:        "291",
				Email:       "flloyd@qrz.com",
				EqCall:      "KJ6RK",
				Web:         "https://www.qrz.com/db/aa7bq",
				Lat:         "34.23456",
				Lon:         "-112.34356",
				ContactedOp: "c/o QRZ LLC",
			},
		},
		{
			name:          "returns raw session error",
			payload:       sessionErrorXML,
			wantErr:       true,
			wantErrString: "Invalid session key",
		},
		{
			name:          "propagates not found",
			payload:       notFoundXML,
			wantErr:       true,
			wantNotFound:  true,
			wantErrString: "Callsign not found",
		},
		{
			name:          "requires callsign element",
			payload:       missingCallXML,
			wantErr:       true,
			wantNotFound:  true,
			wantErrString: "callsign not present in QRZ response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.unmarshalResponse([]byte(tt.payload))

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.wantNotFound && !stderrors.Is(err, smerrors.ErrNotFound) {
					t.Fatalf("expected ErrNotFound, got %v", err)
				}
				if tt.wantErrString != "" && err.Error() != tt.wantErrString {
					t.Fatalf("expected error %q, got %q", tt.wantErrString, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("unexpected station: got %#v want %#v", got, tt.want)
			}
		})
	}
}
