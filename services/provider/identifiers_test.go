package provider

import "testing"

func TestCanonicalIdentifiers(t *testing.T) {
	match, err := ParseProviderMatchID("vf:match:1412174996")
	if err != nil || match != MatchID(1412174996) {
		t.Fatalf("match = %s, err = %v", match, err)
	}
	team, err := ParseProviderTeamUID("sr:competitor:276515")
	if err != nil || team != TeamUID(276515) {
		t.Fatalf("team = %s, err = %v", team, err)
	}
	tournament, err := ParseProviderTournamentID("vf:tournament:14149")
	if err != nil || tournament != TournamentID(14149) {
		t.Fatalf("tournament = %s, err = %v", tournament, err)
	}
	for _, raw := range []string{"", "vf:match:", "sr:competitor:abc", "vf:tournament:0", "other:14149"} {
		if _, err := ParseProviderMatchID(raw); err == nil && raw != "vf:match:1412174996" {
			t.Errorf("ParseProviderMatchID(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestStringishAcceptsObservedStringAndIntegerIDs(t *testing.T) {
	var stringID Stringish
	if err := stringID.UnmarshalJSON([]byte(`"3149311"`)); err != nil {
		t.Fatal(err)
	}
	if stringID.String() != "3149311" {
		t.Fatalf("string ID = %q", stringID)
	}
	if err := stringID.UnmarshalJSON([]byte(`3149369`)); err != nil {
		t.Fatal(err)
	}
	if stringID.String() != "3149369" {
		t.Fatalf("integer ID = %q", stringID)
	}
}

func TestDecimalRejectsInvalidOrNonPositiveOdds(t *testing.T) {
	for _, raw := range []string{"", "0", "0.00", "-1.2", "NaN", "Inf", "1/2"} {
		if _, err := ParseDecimal(raw); err == nil {
			t.Errorf("ParseDecimal(%q) unexpectedly succeeded", raw)
		}
	}
	value, err := ParseDecimal("1.75")
	if err != nil || value.String() != "1.75" {
		t.Fatalf("decimal = %q, err = %v", value, err)
	}
}

func TestNormalizeSpecifiersSortsWithoutDecoding(t *testing.T) {
	raw := " b=2 & a=1 "
	got, err := NormalizeSpecifiers(&raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a=1&b=2" {
		t.Fatalf("normalized specifiers = %q", got)
	}
	got, err = NormalizeSpecifiers(nil)
	if err != nil || got != "" {
		t.Fatalf("nil specifiers = %q, err = %v", got, err)
	}
}
