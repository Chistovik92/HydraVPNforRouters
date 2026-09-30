package singbox

import (
	"regexp"
	"strings"
)

var codeRe = regexp.MustCompile(`(?:^|[\s\[\(|_-])([A-Z]{2})(?:$|[\s\]\)|_-])`)

// CountryOf detects the country of a node from its name: a flag emoji
// (regional indicator pair) or a standalone two-letter upper-case code
// such as "DE" or "[NL]". It returns "" when nothing is found.
func CountryOf(name string) string {
	runes := []rune(name)
	for i := 0; i+1 < len(runes); i++ {
		a, b := runes[i], runes[i+1]
		if isRegional(a) && isRegional(b) {
			return string([]rune{'A' + (a - 0x1F1E6), 'A' + (b - 0x1F1E6)})
		}
	}
	if m := codeRe.FindStringSubmatch(name); m != nil && knownCodes[m[1]] {
		return m[1]
	}
	return ""
}

func isRegional(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// matchCountry compares detected country with a configured list; entries may
// be codes ("DE") or flag emoji.
func matchCountry(list []string, country string) bool {
	if country == "" {
		return false
	}
	for _, c := range list {
		c = strings.TrimSpace(c)
		if strings.EqualFold(c, country) || CountryOf(c) == country {
			return true
		}
	}
	return false
}

// knownCodes limits the plain-text detection to real ISO 3166-1 codes so
// words like "VIP" or "TV" are not taken for countries.
var knownCodes = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range strings.Fields(`AD AE AF AG AL AM AO AR AT AU AZ BA BB BD BE BF BG BH BI BJ BN BO BR BS BT BW BY BZ CA CD CF CG CH CI CL CM CN CO CR CU CV CY CZ DE DJ DK DM DO DZ EC EE EG ER ES ET FI FJ FM FR GA GB GD GE GH GM GN GQ GR GT GW GY HK HN HR HT HU ID IE IL IN IQ IR IS IT JM JO JP KE KG KH KI KM KN KP KR KW KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MG MH MK ML MM MN MO MR MT MU MV MW MX MY MZ NA NE NG NI NL NO NP NR NZ OM PA PE PG PH PK PL PT PW PY QA RO RS RU RW SA SB SC SD SE SG SI SK SL SM SN SO SR SS ST SV SY SZ TD TG TH TJ TL TM TN TO TR TT TV TW TZ UA UG US UY UZ VA VC VE VN VU WS YE ZA ZM ZW`) {
		m[c] = true
	}
	return m
}()
