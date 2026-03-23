package types

type ApiKey struct {
	ID        int64  `json:"id"`
	LogbookID int64  `json:"logbook_id"`
	KeyName   string `json:"key_name"`
	KeyHash   string `json:"key_hash"`
	KeyPrefix string `json:"key_prefix"`
}
