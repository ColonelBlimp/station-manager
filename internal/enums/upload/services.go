// Package upload defines the online service identifiers for QSO forwarding.
package upload

type OnlineService string

// The OnlineServiceQRZ literal is kept as "qrzforwardingservice" for
// backward compatibility with v1 database rows that already use this
// value in the qso_upload.service column. Changing it would require a
// data migration. The value is a v1 artifact where the DI bean name got
// reused as the online-service enum value; v2 may tidy this during the
// forwarder redesign.
const (
	OnlineServiceSM   OnlineService = "sm"
	OnlineServiceQRZ  OnlineService = "qrzforwardingservice"
	OnlineServiceLoTW OnlineService = "lotw"
	OnlineServiceEQSL OnlineService = "eqsl"
	OnlineServiceClub OnlineService = "clublog"
)

var AllowedUploadServices = map[OnlineService]struct{}{
	OnlineServiceSM:   {},
	OnlineServiceQRZ:  {},
	OnlineServiceLoTW: {},
	OnlineServiceEQSL: {},
	OnlineServiceClub: {},
}

func (s OnlineService) Valid() bool {
	_, ok := AllowedUploadServices[s]
	return ok
}

func (s OnlineService) String() string {
	return string(s)
}
