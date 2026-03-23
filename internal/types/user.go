package types

type User struct {
	ID             int64  `json:"id"`
	Callsign       string `json:"callsign" validate:"min=3,max=30,alphanum"`
	PassHash       string `json:"pass_hash" validate:"required"`
	Issuer         string `json:"issuer,omitempty"`
	Subject        string `json:"subject,omitempty"`
	Email          string `json:"email,required"`
	EmailConfirmed bool   `json:"email_confirmed"`
}
