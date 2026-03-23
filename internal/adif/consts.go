package adif

const (
	emptyString   = ""
	JsonStructTag = "json"
	elementFormat = "<%s:%d>%s\n"
	EorStr        = "<EOR>"
	EohStr        = "<EOH>"
	DashStr       = "-"
	ColonStr      = ":"
	Version       = "3.1.5"
	NewLineStr    = "\n"
)

const (
	UserDefQslWanted        = "<USERDEF1:10>QSL_WANTED"             //
	UserDefFwdByEmailStatus = "<USERDEF2:22>SM_FWD_BY_EMAIL_STATUS" // Forwarded by email status
	UserDefFwdByEmailDate   = "<USERDEF3:20:D>SM_FWD_BY_EMAIL_DATE" // Forwarded by email date
	UserDefQsoUploadStatus  = "<USERDEF4:20>SM_QSO_UPLOAD_STATUS"   // QSO upload status
	UserDefQsoUploadDate    = "<USERDEF5:18:D>SM_QSO_UPLOAD_DATE"   // QSO upload date
)

const (
	YesString       = "Y"
	NoString        = "N"
	IgnoreString    = "I"
	RequestedString = "R"
)
