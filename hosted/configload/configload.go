// Package configload loads Gravwell gcfg files with a backward-compatible
// lookback-duration normalization pass.
package configload

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest/config"
)

var (
	assignmentPattern = regexp.MustCompile(`(?i)^([ \t]*)(lookback|initial-lookback)([ \t]*=[ \t]*)(.*)$`)
	durationPattern   = regexp.MustCompile(`(?i)^(?:[0-9]+d)?(?:[0-9]+h)?$`)
	integerPattern    = regexp.MustCompile(`^[+-]?[0-9]+$`)
)

// LoadFile applies the shared lookback syntax before using Gravwell's decoder.
func LoadFile(dst any, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	normalized, err := Normalize(raw)
	if err != nil {
		return fmt.Errorf("load %q: %w", path, err)
	}
	return config.LoadConfigBytes(dst, normalized)
}

// LoadOverlays loads regular .conf files in deterministic lexical order.
func LoadOverlays(dst any, path string) error {
	if path == "" || dst == nil {
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".conf" {
			continue
		}
		file := filepath.Join(path, entry.Name())
		if err := LoadFile(dst, file); err != nil {
			return fmt.Errorf("failed to load %q %w", file, err)
		}
	}
	return nil
}

// Normalize translates Lookback values to integer hours and Initial-Lookback
// values to Go-duration hours. It accepts legacy bare integer hours, Nh, Nd,
// and a day-then-hour combination such as 1d12h.
func Normalize(raw []byte) ([]byte, error) {
	lines := bytes.SplitAfter(raw, []byte{'\n'})
	var out bytes.Buffer
	out.Grow(len(raw))
	for index, line := range lines {
		ending := ""
		body := line
		if bytes.HasSuffix(body, []byte{'\n'}) {
			ending = "\n"
			body = body[:len(body)-1]
			if bytes.HasSuffix(body, []byte{'\r'}) {
				ending = "\r\n"
				body = body[:len(body)-1]
			}
		}
		normalized, err := normalizeLine(string(body))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}
		out.WriteString(normalized)
		out.WriteString(ending)
	}
	return out.Bytes(), nil
}

func normalizeLine(line string) (string, error) {
	parts := assignmentPattern.FindStringSubmatch(line)
	if parts == nil {
		return line, nil
	}
	prefix := parts[1] + parts[2] + parts[3]
	rhs := parts[4]
	leadingLen := len(rhs) - len(strings.TrimLeft(rhs, " \t"))
	leading := rhs[:leadingLen]
	valueAndSuffix := rhs[leadingLen:]
	value, quote, suffix, ok := splitValue(valueAndSuffix)
	if !ok {
		return line, nil
	}

	hours, durationSyntax, err := parseHours(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", parts[2], err)
	}
	if !durationSyntax && !strings.EqualFold(parts[2], "Initial-Lookback") {
		return line, nil
	}
	replacement := strconv.FormatUint(hours, 10)
	if strings.EqualFold(parts[2], "Initial-Lookback") {
		replacement += "h"
	}
	return prefix + leading + quote + replacement + quote + suffix, nil
}

func splitValue(input string) (value, quote, suffix string, ok bool) {
	if input == "" {
		return "", "", "", false
	}
	if input[0] == '\'' || input[0] == '"' {
		quote = input[:1]
		end := strings.Index(input[1:], quote)
		if end < 0 {
			return "", "", "", false
		}
		end++
		value = input[1:end]
		suffix = input[end+1:]
		if !validSuffix(suffix) {
			return "", "", "", false
		}
		return value, quote, suffix, true
	}
	end := len(input)
	for index, char := range input {
		if char == ' ' || char == '\t' || char == '#' || char == ';' {
			end = index
			break
		}
	}
	value = input[:end]
	suffix = input[end:]
	if value == "" || !validSuffix(suffix) {
		return "", "", "", false
	}
	return value, "", suffix, true
}

func validSuffix(suffix string) bool {
	trimmed := strings.TrimSpace(suffix)
	return trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";")
}

func parseHours(value string) (hours uint64, durationSyntax bool, err error) {
	if integerPattern.MatchString(value) {
		if strings.HasPrefix(value, "-") {
			return 0, false, fmt.Errorf("negative value %q not allowed; Lookback and Initial-Lookback must be zero or positive", value)
		}
		hours, err = strconv.ParseUint(strings.TrimPrefix(value, "+"), 10, 64)
		return hours, false, err
	}
	if value == "" || !durationPattern.MatchString(value) {
		return 0, false, fmt.Errorf("invalid duration %q; use whole hours or days such as 24h, 7d, or 1d12h", value)
	}
	lower := strings.ToLower(value)
	dayPart := ""
	hourPart := ""
	if dayEnd := strings.IndexByte(lower, 'd'); dayEnd >= 0 {
		dayPart = lower[:dayEnd]
		lower = lower[dayEnd+1:]
	}
	if strings.HasSuffix(lower, "h") {
		hourPart = strings.TrimSuffix(lower, "h")
	} else if lower != "" {
		return 0, false, fmt.Errorf("invalid duration %q; use whole hours or days such as 24h, 7d, or 1d12h", value)
	}
	var days uint64
	if dayPart != "" {
		days, err = strconv.ParseUint(dayPart, 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("invalid duration %q", value)
		}
	}
	if hourPart != "" {
		hours, err = strconv.ParseUint(hourPart, 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("invalid duration %q", value)
		}
	}
	maxInt := uint64(^uint(0) >> 1)
	if days > maxInt/24 || hours > maxInt-days*24 {
		return 0, false, fmt.Errorf("duration %q overflows integer hours", value)
	}
	return days*24 + hours, true, nil
}
