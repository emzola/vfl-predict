package provider

import (
	"bytes"
	"encoding/json"
)

type ProviderTime struct {
	Document string `json:"_doc"`
	Time     string `json:"time"`
	Date     string `json:"date"`
	Timezone string `json:"tz"`
	TZOffset int64  `json:"tzoffset"`
	UTS      int64  `json:"uts"`
}

type ProviderEnvelopeDoc struct {
	Event     string          `json:"event"`
	MaxAge    *int64          `json:"_maxage"`
	Pages     json.RawMessage `json:"_pages"`
	BornAtUTS *int64          `json:"_dob"`
}

type ProviderFixtureEnvelope struct {
	QueryURL string               `json:"queryUrl"`
	Doc      []ProviderFixtureDoc `json:"doc"`
}

type ProviderFixtureDoc struct {
	ProviderEnvelopeDoc
	Data ProviderFixtureData `json:"data"`
}

type ProviderFixtureData struct {
	Document        string          `json:"_doc"`
	ID              Stringish       `json:"_id"`
	SID             int64           `json:"_sid"`
	TournamentID    int64           `json:"_utid"`
	CurrentSeasonID Stringish       `json:"currentseasonid"`
	Name            string          `json:"name"`
	Abbreviation    string          `json:"abbr"`
	Year            Stringish       `json:"year"`
	Start           ProviderTime    `json:"start"`
	End             ProviderTime    `json:"end"`
	Matches         []ProviderMatch `json:"matches"`
	UnknownFields   map[string]any  `json:"-"`
}

type ProviderMatch struct {
	Document      string                `json:"_doc"`
	ID            int64                 `json:"_id"`
	SeasonID      int64                 `json:"_seasonid"`
	Round         int64                 `json:"round"`
	RoundName     ProviderRoundName     `json:"roundname"`
	Week          string                `json:"week"`
	Time          ProviderTime          `json:"time"`
	Result        *ProviderResult       `json:"result"`
	Periods       ProviderPeriods       `json:"periods"`
	Teams         *ProviderFixtureTeams `json:"teams"`
	Status        json.RawMessage       `json:"status"`
	Postponed     bool                  `json:"postponed"`
	Canceled      bool                  `json:"canceled"`
	Walkover      bool                  `json:"walkover"`
	ToBeAnnounced bool                  `json:"tobeannounced"`
	InLiveScore   bool                  `json:"inlivescore"`
	Comment       *string               `json:"comment"`
	Retired       bool                  `json:"retired"`
	Disqualified  bool                  `json:"disqualified"`
	NeutralGround bool                  `json:"neutralground"`
}

type ProviderRoundName struct {
	ID   int64 `json:"_id"`
	Name any   `json:"name"`
}

type ProviderFixtureTeams struct {
	Home ProviderFixtureTeam `json:"home"`
	Away ProviderFixtureTeam `json:"away"`
}

type ProviderFixtureTeam struct {
	ID         int64   `json:"_id"`
	UID        int64   `json:"uid"`
	Name       string  `json:"name"`
	MediumName string  `json:"mediumname"`
	Abbr       string  `json:"abbr"`
	Nickname   *string `json:"nickname"`
}

type ProviderResult struct {
	Home   *int64  `json:"home"`
	Away   *int64  `json:"away"`
	Period string  `json:"period"`
	Winner *string `json:"winner"`
}

type ProviderScore struct {
	Home int64 `json:"home"`
	Away int64 `json:"away"`
}

type PeriodsKind string

const (
	PeriodsAbsent     PeriodsKind = "absent"
	PeriodsObject     PeriodsKind = "object"
	PeriodsEmptyArray PeriodsKind = "empty_array"
	PeriodsArray      PeriodsKind = "array"
)

type ProviderPeriods struct {
	Kind   PeriodsKind
	Scores *ProviderPeriodScores
	Raw    json.RawMessage
}

type ProviderPeriodScores struct {
	P1 *ProviderScore `json:"p1"`
	FT *ProviderScore `json:"ft"`
}

func (p *ProviderPeriods) UnmarshalJSON(data []byte) error {
	p.Raw = append(p.Raw[:0], data...)
	p.Kind = PeriodsAbsent
	p.Scores = nil
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '[' {
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		if len(values) == 0 {
			p.Kind = PeriodsEmptyArray
		} else {
			p.Kind = PeriodsArray
		}
		return nil
	}
	var scores ProviderPeriodScores
	if err := json.Unmarshal(data, &scores); err != nil {
		return err
	}
	p.Kind = PeriodsObject
	p.Scores = &scores
	return nil
}

type ProviderOddsEnvelope struct {
	QueryURL string            `json:"queryUrl"`
	Doc      []ProviderOddsDoc `json:"doc"`
}

type ProviderOddsDoc struct {
	Data      ProviderOddsData `json:"data"`
	Timestamp int64            `json:"timestamp"`
}

type ProviderOddsData struct {
	Odds []ProviderOddsRecord `json:"odds"`
}

type ProviderOddsRecord struct {
	ID           string            `json:"id"`
	SportID      string            `json:"sport_id"`
	TournamentID string            `json:"tournament_id"`
	Teams        ProviderOddsTeams `json:"teams"`
	Markets      []ProviderMarket  `json:"market"`
}

type ProviderOddsTeams struct {
	Home ProviderOddsTeam `json:"home"`
	Away ProviderOddsTeam `json:"away"`
}

type ProviderOddsTeam struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Abbr string `json:"abbr"`
}

type ProviderMarket struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Specifiers *string           `json:"specifiers"`
	Status     int64             `json:"status"`
	Favourite  *int64            `json:"favourite"`
	Outcomes   []ProviderOutcome `json:"outcome"`
}

type ProviderOutcome struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Abbr   string `json:"abbr"`
	Odds   string `json:"odds"`
	Active int64  `json:"active"`
	Result int64  `json:"result"`
}

type ProviderSeasonListEnvelope struct {
	QueryURL string                  `json:"queryUrl"`
	Doc      []ProviderSeasonListDoc `json:"doc"`
}

type ProviderSeasonListDoc struct {
	ProviderEnvelopeDoc
	Data struct {
		Seasons []ProviderSeason `json:"seasons"`
	} `json:"data"`
}

type ProviderSeason struct {
	ID              Stringish         `json:"_id"`
	TournamentID    int64             `json:"_utid"`
	Current         bool              `json:"current"`
	CurrentSeasonID Stringish         `json:"currentseasonid"`
	Name            string            `json:"name"`
	Abbreviation    string            `json:"abbr"`
	Year            Stringish         `json:"year"`
	Start           ProviderTime      `json:"start"`
	End             ProviderTime      `json:"end"`
	RoundName       ProviderRoundName `json:"roundname"`
}

// The following endpoint shapes are intentionally limited to fields observed
// in the supplied samples. They provide typed envelopes for future adapter
// work without pretending that every provider field or semantic is validated.
type ProviderSeasonMetaEnvelope struct {
	QueryURL string                  `json:"queryUrl"`
	Doc      []ProviderSeasonMetaDoc `json:"doc"`
}

type ProviderSeasonMetaDoc struct {
	ProviderEnvelopeDoc
	Data ProviderSeasonMetaData `json:"data"`
}

type ProviderSeasonMetaData struct {
	Season           ProviderSeasonMetaSeason `json:"season"`
	Sport            map[string]any           `json:"sport"`
	RealCategory     map[string]any           `json:"realcategory"`
	TournamentIDs    []int64                  `json:"tournamentids"`
	TableIDs         []int64                  `json:"tableids"`
	CupIDs           []int64                  `json:"cupids"`
	UniqueTournament map[string]any           `json:"uniquetournament"`
	StatsCoverage    map[string]bool          `json:"statscoverage"`
}

type ProviderSeasonMetaSeason struct {
	ID              Stringish        `json:"_id"`
	TournamentID    int64            `json:"_utid"`
	Name            string           `json:"name"`
	Abbreviation    string           `json:"abbr"`
	CurrentSeasonID Stringish        `json:"currentseasonid"`
	Start           ProviderTime     `json:"start"`
	End             ProviderTime     `json:"end"`
	Year            Stringish        `json:"year"`
	Coverage        map[string]bool  `json:"coverage"`
	H2HDefault      map[string]int64 `json:"h2hdefault"`
}

type ProviderTeamPositionEnvelope struct {
	QueryURL string                    `json:"queryUrl"`
	Doc      []ProviderTeamPositionDoc `json:"doc"`
}

type ProviderTeamPositionDoc struct {
	ProviderEnvelopeDoc
	Data ProviderTeamPositionData `json:"data"`
}

type ProviderTeamPositionData struct {
	TeamCount      int64                            `json:"teamcount"`
	RoundCount     int64                            `json:"roundcount"`
	Teams          map[string]ProviderPositionTeam  `json:"teams"`
	Jersey         map[string]json.RawMessage       `json:"jersey"`
	Season         ProviderPositionSeason           `json:"season"`
	Tables         map[string]ProviderPositionTable `json:"tables"`
	PositionData   []json.RawMessage                `json:"positiondata"`
	PreviousSeason map[string]json.RawMessage       `json:"previousseason"`
	CurrentSeason  map[string]json.RawMessage       `json:"currentseason"`
	MatchesMissing []json.RawMessage                `json:"matchesmissing"`
}

type ProviderPositionTeam struct {
	ID         int64  `json:"_id"`
	UID        int64  `json:"uid"`
	Name       string `json:"name"`
	MediumName string `json:"mediumname"`
	Abbr       string `json:"abbr"`
}

type ProviderPositionSeason struct {
	ID              Stringish    `json:"_id"`
	TournamentID    int64        `json:"_utid"`
	Name            string       `json:"name"`
	Abbreviation    string       `json:"abbr"`
	CurrentSeasonID Stringish    `json:"currentseasonid"`
	Start           ProviderTime `json:"start"`
	End             ProviderTime `json:"end"`
	Year            Stringish    `json:"year"`
}

type ProviderPositionTable struct {
	ID           Stringish    `json:"_id"`
	TournamentID int64        `json:"tournamentid"`
	Name         string       `json:"name"`
	Abbreviation string       `json:"abbr"`
	MaxRounds    Stringish    `json:"maxrounds"`
	SeasonID     Stringish    `json:"seasonid"`
	SeasonType   Stringish    `json:"seasontype"`
	Start        ProviderTime `json:"start"`
	End          ProviderTime `json:"end"`
	RoundByRound bool         `json:"roundbyround"`
}

type ProviderLiveTableEnvelope struct {
	QueryURL string                 `json:"queryUrl"`
	Doc      []ProviderLiveTableDoc `json:"doc"`
}

type ProviderLiveTableDoc struct {
	ProviderEnvelopeDoc
	Data ProviderLiveTableData `json:"data"`
}

type ProviderLiveTableData struct {
	ID           Stringish          `json:"_id"`
	SeasonID     Stringish          `json:"seasonid"`
	MaxRounds    int64              `json:"maxrounds"`
	CurrentRound int64              `json:"currentround"`
	Name         string             `json:"name"`
	Abbreviation string             `json:"abbr"`
	TotalRows    int64              `json:"totalrows"`
	Tournament   map[string]any     `json:"tournament"`
	RealCategory map[string]any     `json:"realcategory"`
	Rules        map[string]any     `json:"rules"`
	TableRows    []ProviderTableRow `json:"tablerows"`
}

type ProviderTableRow struct {
	ID                int64               `json:"_id"`
	Team              ProviderFixtureTeam `json:"team"`
	Position          int64               `json:"pos"`
	Total             int64               `json:"total"`
	WinTotal          int64               `json:"winTotal"`
	DrawTotal         int64               `json:"drawTotal"`
	LossTotal         int64               `json:"lossTotal"`
	GoalsForTotal     int64               `json:"goalsForTotal"`
	GoalsAgainstTotal int64               `json:"goalsAgainstTotal"`
	PointsTotal       int64               `json:"pointsTotal"`
	PointsGivenTotal  Stringish           `json:"pointsGivenTotal"`
	GoalDiffTotal     int64               `json:"goalDiffTotal"`
	CurrentlyPlaying  int64               `json:"currentlyPlaying"`
}
