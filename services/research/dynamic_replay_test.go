package research

import (
	"testing"
	"time"
)

func TestDynamicReplayUsesChronologicalCutoffs(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	matches := []MatchRecord{
		dynamicMatch(1, "2024", 1, base, 1, 2, 2, 0),
		dynamicMatch(2, "2024", 2, base.Add(24*time.Hour), 2, 1, 0, 1),
		dynamicMatch(3, "2025", 1, base.AddDate(1, 0, 0), 1, 2, 1, 0),
		dynamicMatch(4, "2025", 1, base.AddDate(1, 0, 0).Add(time.Hour), 3, 4, 0, 0),
		dynamicMatch(5, "2025", 2, base.AddDate(1, 0, 0).Add(24*time.Hour), 1, 2, 0, 3),
		dynamicMatch(6, "2025", 2, base.AddDate(1, 0, 0).Add(25*time.Hour), 3, 4, 4, 0),
	}
	dataset := NewDataset(matches, []SeasonRecord{{ID: "2024", StartUTC: base, EndUTC: base.Add(48 * time.Hour)}, {ID: "2025", StartUTC: base.AddDate(1, 0, 0), EndUTC: base.AddDate(1, 0, 0).Add(48 * time.Hour)}})
	config := DefaultDynamicReplayConfig()
	config.MinTrainingSeasons = 1
	config.BootstrapReplicates = 0
	original, err := RunDynamicBayesianReplay(dataset, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Predictions) != 4 {
		t.Fatalf("replay predictions=%d want 4", len(original.Predictions))
	}
	mutated := append([]MatchRecord(nil), matches...)
	mutated[4].HomeScore = intPointer(7)
	mutated[4].AwayScore = intPointer(7)
	mutated[5].HomeScore = intPointer(0)
	mutated[5].AwayScore = intPointer(8)
	changed, err := RunDynamicBayesianReplay(NewDataset(mutated, dataset.Seasons), config)
	if err != nil {
		t.Fatal(err)
	}
	for index := range original.Predictions {
		if original.Predictions[index].Round == 1 && original.Predictions[index].Probability != changed.Predictions[index].Probability {
			t.Fatalf("future round changed earlier prediction at index %d: %#v vs %#v", index, original.Predictions[index].Probability, changed.Predictions[index].Probability)
		}
	}
	for _, prediction := range original.Predictions {
		if err := prediction.Probability.Validate(1e-10); err != nil {
			t.Fatal(err)
		}
		if prediction.BTTSYesProbability < 0 || prediction.BTTSYesProbability > 1 || prediction.Over25Probability < 0 || prediction.Over25Probability > 1 {
			t.Fatalf("invalid secondary probability: %#v", prediction)
		}
	}
}

func TestDynamicReplayEarlySeasonUsesRegularizedPrior(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	dataset := NewDataset([]MatchRecord{
		dynamicMatch(1, "2024", 1, base, 1, 2, 0, 0),
		dynamicMatch(2, "2025", 1, base.AddDate(1, 0, 0), 1, 2, 0, 1),
		dynamicMatch(3, "2026", 1, base.AddDate(2, 0, 0), 1, 2, 1, 0),
	}, nil)
	config := DefaultDynamicReplayConfig()
	config.MinTrainingSeasons = 2
	config.BootstrapReplicates = 0
	result, err := RunDynamicBayesianReplay(dataset, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Predictions) != 1 {
		t.Fatalf("early-season replay predictions=%d want 1", len(result.Predictions))
	}
	if result.Predictions[0].Diagnostics.SeasonMatchesUsed != 0 {
		t.Fatalf("round-one prediction used current-season results: %#v", result.Predictions[0].Diagnostics)
	}
}
