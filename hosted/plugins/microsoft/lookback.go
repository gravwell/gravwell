package microsoft

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// LookbackHours accepts integer hours and explicit whole-day/hour units through
// the standard gcfg decoder.
// Config.Verify retains the source-specific 1..672 hour bound.
type LookbackHours int

var lookbackUnits = regexp.MustCompile(`^(?:([0-9]+)d)?(?:([0-9]+)h)?$`)

func (h *LookbackHours) UnmarshalText(raw []byte) error {
	value := strings.ToLower(strings.TrimSpace(string(raw)))
	if n, err := strconv.Atoi(value); err == nil {
		*h = LookbackHours(n)
		return nil
	}
	parts := lookbackUnits.FindStringSubmatch(value)
	if parts == nil || (parts[1] == "" && parts[2] == "") {
		return errors.New("Lookback requires integer hours, Nh, Nd, or NdNh")
	}
	maximum := uint64(^uint(0) >> 1)
	var total uint64
	for i, part := range parts[1:] {
		if part == "" {
			continue
		}
		n, err := strconv.ParseUint(part, 10, 64)
		multiplier := uint64(1)
		if i == 0 {
			multiplier = 24
		}
		if err != nil || n > (maximum-total)/multiplier {
			return errors.New("Lookback duration overflows integer hours")
		}
		total += n * multiplier
	}
	if total == 0 {
		return errors.New("Lookback duration must be positive")
	}
	*h = LookbackHours(total)
	return nil
}
