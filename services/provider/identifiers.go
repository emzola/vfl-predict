package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The provider exposes IDs in several textual namespaces. These types keep
// those namespaces out of business logic while retaining the raw provider ID
// on normalized records.
type SeasonID string

type TournamentID uint64

type MatchID uint64

type TeamUID uint64

type RoundNumber uint8

func (s SeasonID) String() string { return string(s) }

func (t TournamentID) String() string { return strconv.FormatUint(uint64(t), 10) }

func (m MatchID) String() string { return strconv.FormatUint(uint64(m), 10) }

func (t TeamUID) String() string { return strconv.FormatUint(uint64(t), 10) }

func (r RoundNumber) Int() int { return int(r) }

func NewMatchID(value int64) (MatchID, error) {
	if value <= 0 {
		return 0, fmt.Errorf("match ID must be positive, got %d", value)
	}
	return MatchID(value), nil
}

func NewTeamUID(value int64) (TeamUID, error) {
	if value <= 0 {
		return 0, fmt.Errorf("team UID must be positive, got %d", value)
	}
	return TeamUID(value), nil
}

func NewTournamentID(value int64) (TournamentID, error) {
	if value <= 0 {
		return 0, fmt.Errorf("tournament ID must be positive, got %d", value)
	}
	return TournamentID(value), nil
}

func NewRoundNumber(value int64) (RoundNumber, error) {
	if value < 1 || value > 30 {
		return 0, fmt.Errorf("round must be between 1 and 30, got %d", value)
	}
	return RoundNumber(value), nil
}

func ParseProviderMatchID(raw string) (MatchID, error) {
	value, err := parseNamespacedUint(raw, "vf:match:", "match ID")
	return MatchID(value), err
}

func ParseProviderTeamUID(raw string) (TeamUID, error) {
	value, err := parseNamespacedUint(raw, "sr:competitor:", "team UID")
	return TeamUID(value), err
}

func ParseProviderTournamentID(raw string) (TournamentID, error) {
	value, err := parseNamespacedUint(raw, "vf:tournament:", "tournament ID")
	return TournamentID(value), err
}

func parseNamespacedUint(raw, prefix, label string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, prefix) {
		return 0, fmt.Errorf("%s %q does not have prefix %q", label, raw, prefix)
	}
	suffix := strings.TrimPrefix(raw, prefix)
	if suffix == "" {
		return 0, fmt.Errorf("%s %q has an empty numeric suffix", label, raw)
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%s %q has a non-numeric suffix", label, raw)
		}
	}
	value, err := strconv.ParseUint(suffix, 10, 64)
	if err != nil || value == 0 {
		if err == nil {
			err = errors.New("zero is not a valid canonical identifier")
		}
		return 0, fmt.Errorf("invalid %s %q: %w", label, raw, err)
	}
	return value, nil
}

// Stringish preserves the string representation of a provider ID that is
// observed as either a JSON string or JSON integer in different objects.
type Stringish string

func (s *Stringish) UnmarshalJSON(data []byte) error {
	value, err := decodeStringish(data)
	if err != nil {
		return err
	}
	*s = Stringish(value)
	return nil
}

func (s Stringish) String() string { return string(s) }

func decodeStringish(data []byte) (string, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, "\"") {
		var value string
		if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
			return "", fmt.Errorf("decode string ID: %w", err)
		}
		return value, nil
	}
	if strings.HasPrefix(trimmed, "-") {
		return "", fmt.Errorf("negative string ID %q", trimmed)
	}
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("expected JSON string or integer ID, got %q", trimmed)
		}
	}
	return trimmed, nil
}

var decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// Decimal is a validated, non-floating-point decimal representation. Its
// string form is what should be persisted; math/big.Rat is used only to prove
// that the value is numerically valid.
type Decimal string

func (d Decimal) String() string { return string(d) }

func (d Decimal) IsZero() bool { return string(d) == "0" || strings.Trim(string(d), "0.") == "" }

func (d Decimal) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(d))
}

func ParseDecimal(raw string) (Decimal, error) {
	value := strings.TrimSpace(raw)
	if !decimalPattern.MatchString(value) {
		return "", fmt.Errorf("invalid decimal odds %q", raw)
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || rational.Sign() <= 0 {
		return "", fmt.Errorf("odds must be finite and greater than zero, got %q", raw)
	}
	return Decimal(value), nil
}

// NormalizeSpecifiers returns a deterministic text key. It deliberately does
// not URL-decode provider values because the provider's encoding semantics are
// not yet validated. Blank and null specifiers normalize to the empty key.
func NormalizeSpecifiers(raw *string) (string, error) {
	if raw == nil {
		return "", nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return "", nil
	}
	pairs := make([]string, 0)
	for _, part := range strings.Split(value, "&") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, val := part, ""
		if index := strings.IndexByte(part, '='); index >= 0 {
			key, val = strings.TrimSpace(part[:index]), strings.TrimSpace(part[index+1:])
		}
		if key == "" {
			return "", fmt.Errorf("specifier %q has an empty key", value)
		}
		pairs = append(pairs, key+"="+val)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&"), nil
}

type MarketIdentity struct {
	MatchID    MatchID `json:"match_id"`
	MarketID   int64   `json:"market_id"`
	Specifiers string  `json:"specifiers"`
	OutcomeID  string  `json:"outcome_id"`
}

func (m MarketIdentity) String() string {
	return fmt.Sprintf("%s/%d/%s/%s", m.MatchID, m.MarketID, m.Specifiers, m.OutcomeID)
}
