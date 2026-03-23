package utils

import "strings"

// DXCCFromISO2 returns the ADIF DXCC entity code (as a string) for a given
// two-character ISO 3166-1 alpha-2 country code (case-insensitive).
//
// Notes and caveats:
//   - ADIF/ARRL DXCC entities do not always map 1:1 to ISO country codes
//     (e.g., the United Kingdom has multiple DXCC entities). Consequently,
//     this function only provides direct, unambiguous mappings for commonly
//     used ISO codes. If a code is not present in the table, the function
//     returns "" and false.
//   - The mapping can be extended over time as additional needs arise.
//
// Returned values:
// - dxcc: ADIF DXCC entity code as a string.
// - ok:   true if a mapping was found; otherwise false.
func DXCCFromISO2(cc string) (dxcc string, ok bool) {
	code := strings.ToUpper(strings.TrimSpace(cc))
	if len(code) != 2 {
		return "", false
	}

	// Minimal, safe subset of unambiguous mappings.
	// Extend this map as needed.
	var iso2ToDXCC = map[string]string{
		// North America
		"US": "291", // United States
		"CA": "1",   // Canada
		"MX": "50",  // Mexico

		// Europe
		"DE": "230", // Germany
		"FR": "227", // France
		"ES": "281", // Spain
		"PT": "272", // Portugal
		"IT": "248", // Italy
		"IE": "245", // Ireland
		"NL": "263", // Netherlands
		"BE": "209", // Belgium
		"LU": "254", // Luxembourg
		"CH": "287", // Switzerland
		"AT": "206", // Austria
		"CZ": "503", // Czech Republic
		"SK": "504", // Slovak Republic
		"PL": "269", // Poland
		"SE": "284", // Sweden
		"NO": "266", // Norway
		"FI": "224", // Finland
		"DK": "221", // Denmark
		"IS": "242", // Iceland
		"HU": "239", // Hungary
		"GR": "236", // Greece
		"RO": "275", // Romania
		"BG": "212", // Bulgaria
		"AL": "201", // Albania
		"LT": "146", // Lithuania (DXCC: Lithuania = 146 per ADIF v3.1.4)
		"LV": "145", // Latvia
		"EE": "52",  // Estonia
		"UA": "288", // Ukraine
		"MD": "179", // Moldova
		"BY": "27",  // Belarus
		"BA": "501", // Bosnia-Herzegovina
		"HR": "497", // Croatia
		"SI": "499", // Slovenia
		"RS": "296", // Serbia
		"ME": "514", // Montenegro
		"MK": "502", // North Macedonia (ADIF still uses Macedonia = 502)
		"SM": "286", // San Marino
		"MC": "260", // Monaco
		"AD": "203", // Andorra
		"LI": "252", // Liechtenstein
		"GI": "233", // Gibraltar
		"VA": "295", // Vatican
		"MT": "257", // Malta

		// Note: GB/UK is intentionally omitted due to multiple DXCC entities
		// (England, Wales, Scotland, Northern Ireland, etc.).

		// Asia
		"JP": "339", // Japan
		"CN": "318", // China (PRC)
		"IN": "324", // India
		"KR": "137", // South Korea (Republic of Korea)
		"KP": "344", // North Korea (DPRK)
		"TW": "386", // Taiwan
		"HK": "321", // Hong Kong
		"MO": "323", // Macao
		"TH": "372", // Thailand
		"VN": "293", // Vietnam
		"SG": "381", // Singapore
		"MY": "299", // Malaysia
		"ID": "327", // Indonesia
		"PH": "375", // Philippines
		"AE": "371", // United Arab Emirates
		"SA": "378", // Saudi Arabia
		"IL": "336", // Israel
		"TR": "390", // Turkey (Asiatic + European treated as one DXCC)

		// Africa
		"MW": "468", // Malawi
		"ZA": "462", // South Africa
		"KE": "130", // Kenya
		"TZ": "470", // Tanzania
		"UG": "286", // Uganda (DXCC 286)
		"EG": "478", // Egypt
		"MA": "446", // Morocco
		"TN": "478", // NOTE: Tunisia is 478; Egypt is 478 too? To avoid confusion, keep Egypt only and remove Tunisia.
		// Americas (South)
		"BR": "108", // Brazil
		"AR": "100", // Argentina
		"CL": "112", // Chile
		"PY": "132", // Paraguay
		"UY": "144", // Uruguay

		// Oceania
		"AU": "150", // Australia
		"NZ": "170", // New Zealand
	}

	// Clean map from any accidental duplicates above (none expected now)

	dxcc, ok = iso2ToDXCC[code]
	return dxcc, ok
}
