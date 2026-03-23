package bands

type Band string

const (
	Band160 Band = "160m"
	Band80  Band = "80m"
	Band60  Band = "60m"
	Band40  Band = "40m"
	Band30  Band = "30m"
	Band20  Band = "20m"
	Band17  Band = "17m"
	Band15  Band = "15m"
	Band12  Band = "12m"
	Band10  Band = "10m"
	Band6   Band = "6m"
)

func IsValidBand(s string) bool {
	switch Band(s) {
	case Band160, Band80, Band60, Band40, Band30, Band20, Band17, Band15, Band12, Band10, Band6:
		return true
	default:
		return false
	}
}

func (b Band) String() string {
	return string(b)
}
