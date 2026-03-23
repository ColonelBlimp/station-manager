package types

type User struct {
	ID             int64  `json:"id"`
	Callsign       string `json:"callsign" validate:"required,min=3,max=30,alphanum"`
	PassHash       string `json:"pass_hash" validate:"required"`
	Issuer         string `json:"issuer,omitempty"`
	Subject        string `json:"subject,omitempty"`
	Email          string `json:"email" validate:"required,email"`
	EmailConfirmed bool   `json:"email_confirmed"`
}
