// Package bands defines the amateur radio band constants per the ADIF spec.
//
// HF + 6m through the common VHF/UHF bands (review 2026-06-19): the accepted
// set must match what the frequency→band mappers emit (internal/utils
// FrequencyToBand and the logging SPA's frequencyToBand) — otherwise a dial
// frequency labelled "2m" by those mappers would be rejected here at submit.
package bands

type Band string

const (
	Band160  Band = "160m"
	Band80   Band = "80m"
	Band60   Band = "60m"
	Band40   Band = "40m"
	Band30   Band = "30m"
	Band20   Band = "20m"
	Band17   Band = "17m"
	Band15   Band = "15m"
	Band12   Band = "12m"
	Band10   Band = "10m"
	Band6    Band = "6m"
	Band4    Band = "4m"
	Band2    Band = "2m"
	Band125  Band = "1.25m"
	Band70cm Band = "70cm"
	Band33cm Band = "33cm"
	Band23cm Band = "23cm"
)

func IsValidBand(s string) bool {
	switch Band(s) {
	case Band160, Band80, Band60, Band40, Band30, Band20, Band17, Band15, Band12, Band10, Band6,
		Band4, Band2, Band125, Band70cm, Band33cm, Band23cm:
		return true
	default:
		return false
	}
}

func (b Band) String() string {
	return string(b)
}
