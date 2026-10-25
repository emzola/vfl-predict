package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SeasonSummary is the canonical, discovery-time representation of a season.
// Current is taken only from the season discovery response; it is not inferred
// from dates or from a separate current-season metadata field.
type SeasonSummary struct {
	ID              SeasonID
	TournamentID    TournamentID
	Current         bool
	CurrentSeasonID *SeasonID
	Name            string
	Abbreviation    string
	ProviderYear    string
	StartUTC        time.Time
	EndUTC          time.Time
}

type SeasonDiscovery struct {
	QueryURL string
	Seasons  []SeasonSummary
}

func ParseSeasonDiscovery(payload []byte) (SeasonDiscovery, error) {
	var envelope ProviderSeasonListEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return SeasonDiscovery{}, fmt.Errorf("decode season discovery envelope: %w", err)
	}
	if strings.TrimSpace(envelope.QueryURL) == "" {
		return SeasonDiscovery{}, fmt.Errorf("season discovery envelope is missing queryUrl")
	}
	if len(envelope.Doc) != 1 {
		return SeasonDiscovery{}, fmt.Errorf("season discovery envelope must contain exactly one doc, got %d", len(envelope.Doc))
	}
	if len(envelope.Doc[0].Data.Seasons) == 0 {
		return SeasonDiscovery{}, fmt.Errorf("season discovery response contains no seasons")
	}

	seasons := make([]SeasonSummary, 0, len(envelope.Doc[0].Data.Seasons))
	seen := make(map[SeasonID]struct{}, len(envelope.Doc[0].Data.Seasons))
	for index, raw := range envelope.Doc[0].Data.Seasons {
		id := SeasonID(strings.TrimSpace(raw.ID.String()))
		if id == "" {
			return SeasonDiscovery{}, fmt.Errorf("season[%d] is missing _id", index)
		}
		if _, exists := seen[id]; exists {
			return SeasonDiscovery{}, fmt.Errorf("duplicate season ID %s", id)
		}
		seen[id] = struct{}{}
		tournamentID, err := NewTournamentID(raw.TournamentID)
		if err != nil {
			return SeasonDiscovery{}, fmt.Errorf("season %s: %w", id, err)
		}
		start, err := providerTimeUTC(raw.Start, "season start")
		if err != nil {
			return SeasonDiscovery{}, fmt.Errorf("season %s: %w", id, err)
		}
		end, err := providerTimeUTC(raw.End, "season end")
		if err != nil {
			return SeasonDiscovery{}, fmt.Errorf("season %s: %w", id, err)
		}
		var currentSeasonID *SeasonID
		if value := strings.TrimSpace(raw.CurrentSeasonID.String()); value != "" {
			canonical := SeasonID(value)
			currentSeasonID = &canonical
		}
		seasons = append(seasons, SeasonSummary{
			ID:              id,
			TournamentID:    tournamentID,
			Current:         raw.Current,
			CurrentSeasonID: currentSeasonID,
			Name:            raw.Name,
			Abbreviation:    raw.Abbreviation,
			ProviderYear:    raw.Year.String(),
			StartUTC:        start,
			EndUTC:          end,
		})
	}
	return SeasonDiscovery{QueryURL: envelope.QueryURL, Seasons: seasons}, nil
}
