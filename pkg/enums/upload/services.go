package upload

import "github.com/Station-Manager/types"

type OnlineService string

const (
	OnlineServiceSM   OnlineService = "sm"
	OnlineServiceQRZ  OnlineService = types.QrzForwardingServiceName
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
