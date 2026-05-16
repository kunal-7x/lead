package phone

import (
	"fmt"
	"strings"

	"github.com/nyaruka/phonenumbers"
)

// Normalize converts a raw phone string to E.164 format.
// defaultCountry is an ISO-3166-1 alpha-2 code (e.g. "IN", "US").
func Normalize(raw, defaultCountry string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty phone number")
	}

	// Try to parse with the given default country.
	num, err := phonenumbers.Parse(raw, defaultCountry)
	if err != nil {
		return "", fmt.Errorf("parse phone %q: %w", raw, err)
	}

	if !phonenumbers.IsValidNumber(num) {
		return "", fmt.Errorf("invalid phone number %q", raw)
	}

	return phonenumbers.Format(num, phonenumbers.E164), nil
}
