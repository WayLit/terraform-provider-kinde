package kindeapi

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nyaruka/phonenumbers"
)

// parsePhone splits a phone number in international format, such as
// "+61412345678", into the national number ("412345678") and the lower-case
// country ID ("au") that Kinde's create-identity endpoint expects. It is
// ported from github.com/nxt-fwd/kinde-go's internal/phone package.
func parsePhone(number string) (national, countryID string, err error) {
	num, err := phonenumbers.Parse(number, "")
	if err != nil {
		return "", "", fmt.Errorf("invalid phone number %q: %w", number, err)
	}
	if !phonenumbers.IsValidNumber(num) {
		return "", "", fmt.Errorf("invalid phone number %q", number)
	}
	national = strconv.FormatUint(num.GetNationalNumber(), 10)
	countryID = strings.ToLower(phonenumbers.GetRegionCodeForNumber(num))
	return national, countryID, nil
}
