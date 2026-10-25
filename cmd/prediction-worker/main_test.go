package main

import (
	"testing"
	"time"

	"bet9ja-vfl/services/prediction"
)

func TestShouldFreezeEntersWindowBeforeEngineCutoff(t *testing.T) {
	now := time.Date(2026, time.October, 25, 20, 0, 0, 0, time.UTC)
	config := prediction.DefaultConfig()
	target := roundTarget{SeasonID: "season-1", Round: 4, EarliestKickoffAt: now.Add(10 * time.Minute)}

	if shouldFreeze(target, now, config, false, true, 90*time.Second) {
		t.Fatal("round should remain provisional outside the freeze window")
	}
	if !shouldFreeze(target, target.EarliestKickoffAt.Add(-2*time.Minute), config, false, true, 90*time.Second) {
		t.Fatal("round should freeze once it enters the lead window")
	}
}

func TestShouldFreezeHonorsForceAndAutoFreezeFlag(t *testing.T) {
	now := time.Date(2026, time.October, 25, 20, 0, 0, 0, time.UTC)
	config := prediction.DefaultConfig()
	target := roundTarget{SeasonID: "season-1", Round: 4, EarliestKickoffAt: now.Add(time.Hour)}

	if shouldFreeze(target, now, config, false, false, 90*time.Second) {
		t.Fatal("auto-freeze disabled should not freeze a round")
	}
	if !shouldFreeze(target, now, config, true, false, 90*time.Second) {
		t.Fatal("explicit freeze should always freeze the requested round")
	}
}

func TestDurationFromEnvSecondsUsesFallback(t *testing.T) {
	t.Setenv("PREDICTION_TEST_DURATION_SECONDS", "")
	fallback := 30 * time.Second
	value, err := durationFromEnvSeconds("PREDICTION_TEST_DURATION_SECONDS", fallback)
	if err != nil {
		t.Fatalf("durationFromEnvSeconds returned an error: %v", err)
	}
	if value != fallback {
		t.Fatalf("durationFromEnvSeconds() = %s, want %s", value, fallback)
	}
}

func TestDurationFromEnvSecondsRejectsInvalidValues(t *testing.T) {
	t.Setenv("PREDICTION_TEST_DURATION_SECONDS", "0")
	if _, err := durationFromEnvSeconds("PREDICTION_TEST_DURATION_SECONDS", time.Second); err == nil {
		t.Fatal("durationFromEnvSeconds should reject non-positive values")
	}
}
