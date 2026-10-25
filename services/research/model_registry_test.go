package research

import "testing"

func TestModelPromotionTransitions(t *testing.T) {
	valid := []struct {
		current string
		next    string
	}{
		{"candidate", "candidate"},
		{"candidate", "approved"},
		{"candidate", "retired"},
		{"approved", "approved"},
		{"approved", "active"},
		{"approved", "retired"},
		{"active", "active"},
		{"active", "retired"},
		{"retired", "retired"},
	}
	for _, value := range valid {
		if !validPromotionTransition(value.current, value.next) {
			t.Fatalf("expected valid transition %s -> %s", value.current, value.next)
		}
	}
	for _, value := range [][2]string{
		{"retired", "candidate"},
		{"retired", "approved"},
		{"retired", "active"},
		{"active", "approved"},
		{"candidate", "active"},
	} {
		if validPromotionTransition(value[0], value[1]) {
			t.Fatalf("expected invalid transition %s -> %s", value[0], value[1])
		}
	}
}
