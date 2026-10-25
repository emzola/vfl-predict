package research

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"bet9ja-vfl/services/provider"
)

// The live Phase 1 probe observed market id 1 in the returned catalog, but did
// not establish a provider-wide market-ID contract. Keep this as an explicit
// adapter constant and require the outcome labels/IDs to form a complete 1X2
// line before using it.
const OneXTwoMarketID int64 = 1

func FindOneXTwoOdds(match MatchRecord, allowUnverified bool) (OddsLine, bool) {
	return findOneXTwoOdds(match, time.Time{}, allowUnverified, false)
}

// FindOneXTwoOddsAt is the point-in-time-safe selector. A quote must have an
// explicit verified flag and an observed timestamp at or before the cutoff.
// A provider response timestamp alone is not sufficient evidence.
func FindOneXTwoOddsAt(match MatchRecord, cutoff time.Time) (OddsLine, bool) {
	return findOneXTwoOdds(match, cutoff, false, true)
}

func findOneXTwoOdds(match MatchRecord, cutoff time.Time, allowUnverified, enforceAsOf bool) (OddsLine, bool) {
	type candidate struct {
		key   OutcomeKey
		quote OddsQuote
	}
	candidates := make([]candidate, 0, 3)
	for _, quote := range match.Odds {
		if quote.MarketID != OneXTwoMarketID || strings.TrimSpace(quote.Specifiers) != "" || quote.Odds <= 0 || math.IsNaN(quote.Odds) || math.IsInf(quote.Odds, 0) {
			continue
		}
		if !allowUnverified && !quote.PointInTimeVerified {
			continue
		}
		if enforceAsOf {
			if !quote.PointInTimeVerified || quote.ObservedAt.IsZero() || quote.ObservedAt.After(cutoff) {
				continue
			}
		}
		key, ok := classifyOutcome(quote, match)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{key: key, quote: quote})
	}
	if len(candidates) == 0 {
		return OddsLine{}, false
	}
	// If multiple snapshots are present, use the latest observed quote for each
	// outcome. Ties are broken by provider source timestamp and raw odds so the
	// choice is deterministic.
	latest := make(map[OutcomeKey]OddsQuote, 3)
	for _, candidate := range candidates {
		previous, exists := latest[candidate.key]
		if !exists || quoteAfter(candidate.quote, previous) {
			latest[candidate.key] = candidate.quote
		}
	}
	home, homeOK := latest[OutcomeHome]
	draw, drawOK := latest[OutcomeDraw]
	away, awayOK := latest[OutcomeAway]
	if !homeOK || !drawOK || !awayOK {
		return OddsLine{}, false
	}
	overround := 1/home.Odds + 1/draw.Odds + 1/away.Odds
	if overround <= 0 || math.IsNaN(overround) || math.IsInf(overround, 0) {
		return OddsLine{}, false
	}
	line := OddsLine{
		Home:                home.Odds,
		Draw:                draw.Odds,
		Away:                away.Odds,
		HomeQuote:           home,
		DrawQuote:           draw,
		AwayQuote:           away,
		Overround:           overround,
		PointInTimeVerified: home.PointInTimeVerified && draw.PointInTimeVerified && away.PointInTimeVerified,
		ObservationKind:     commonObservationKind(home, draw, away),
	}
	return line, true
}

func quoteAfter(left, right OddsQuote) bool {
	if !left.ObservedAt.Equal(right.ObservedAt) {
		return left.ObservedAt.After(right.ObservedAt)
	}
	if !left.SourceTimestampAt.Equal(right.SourceTimestampAt) {
		return left.SourceTimestampAt.After(right.SourceTimestampAt)
	}
	return left.RawOdds > right.RawOdds
}

func commonObservationKind(quotes ...OddsQuote) string {
	if len(quotes) == 0 {
		return "unknown"
	}
	kind := strings.TrimSpace(quotes[0].ObservationKind)
	if kind == "" {
		kind = "unknown"
	}
	for _, quote := range quotes[1:] {
		if strings.TrimSpace(quote.ObservationKind) != kind {
			return "mixed"
		}
	}
	return kind
}

func classifyOutcome(quote OddsQuote, match MatchRecord) (OutcomeKey, bool) {
	id := strings.ToLower(strings.TrimSpace(quote.OutcomeID))
	name := normalizedText(quote.OutcomeName)
	abbr := normalizedText(quote.OutcomeAbbreviation)
	if id == "1" || id == "h" || id == "home" || strings.Contains(name, "home") || strings.Contains(abbr, "home") {
		return OutcomeHome, true
	}
	if id == "x" || id == "d" || id == "draw" || strings.Contains(name, "draw") || strings.Contains(name, "tie") || strings.Contains(abbr, "draw") {
		return OutcomeDraw, true
	}
	if id == "2" || id == "a" || id == "away" || strings.Contains(name, "away") || strings.Contains(abbr, "away") {
		return OutcomeAway, true
	}
	// Some provider variants use team names instead of 1/X/2 labels. This is a
	// deliberately narrow fallback and still requires all three classes.
	if team := normalizedText(match.HomeTeamName); team != "" && (name == team || abbr == normalizedText(match.HomeTeamName)) {
		return OutcomeHome, true
	}
	if team := normalizedText(match.AwayTeamName); team != "" && (name == team || abbr == normalizedText(match.AwayTeamName)) {
		return OutcomeAway, true
	}
	return "", false
}

func (line OddsLine) Validate() error {
	if line.Home <= 0 || line.Draw <= 0 || line.Away <= 0 {
		return fmt.Errorf("1X2 odds must be positive")
	}
	if line.Overround <= 0 {
		return fmt.Errorf("overround must be positive")
	}
	probabilities := line.ImpliedProbabilities()
	return probabilities.Validate(1e-9)
}

// SortedOdds returns a stable copy useful when serializing provenance.
func (m MatchRecord) SortedOdds() []OddsQuote {
	quotes := append([]OddsQuote(nil), m.Odds...)
	sort.SliceStable(quotes, func(i, j int) bool {
		if quotes[i].MarketID != quotes[j].MarketID {
			return quotes[i].MarketID < quotes[j].MarketID
		}
		if quotes[i].Specifiers != quotes[j].Specifiers {
			return quotes[i].Specifiers < quotes[j].Specifiers
		}
		if quotes[i].OutcomeID != quotes[j].OutcomeID {
			return quotes[i].OutcomeID < quotes[j].OutcomeID
		}
		return quoteAfter(quotes[i], quotes[j])
	})
	return quotes
}

func probabilitiesFromOdds(line OddsLine) Probability {
	return line.ImpliedProbabilities()
}

// `provider` is imported here to keep compile-time pressure on the canonical
// identifier types in this package even when this file is built standalone.
var _ provider.MatchID
