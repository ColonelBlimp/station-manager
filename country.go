package types

type Country struct {
	ID                int64
	Name              string `json:"name" hamnut:"countryName"`
	Prefix            string `json:"prefix" hamnut:"prefix"`
	Ccode             string `json:"ccode" hamnut:"countryCode"`
	Continent         string `json:"continent" hamnut:"continent"`
	CQZone            string `json:"cq_zone" hamnut:"cqZone"`
	ITUZone           string `json:"itu_zone" hamnut:"ituZone"`
	DXCCPrefix        string `json:"dxcc_prefix" hamnut:"primaryDXCCPrefix"`
	TimeOffset        string `json:"time_offset" hamnut:"timeOffset"`
	ShortPathDistance string `json:"short_path_distance"`
	LongPathDistance  string `json:"long_path_distance"`
	ShortPathBearing  string `json:"short_path_bearing"`
	LongPathBearing   string `json:"long_path_bearing"`
	IsNewEntity       bool   `json:"is_new_entity"` // Indicates if this QSO is with a new country for the logging station
	LocalTime         string `json:"local_time"`
}
