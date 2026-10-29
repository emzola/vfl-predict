package autobet

import "testing"

func TestCredentialEncryptionRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	encoded, err := encryptCredentialValue(key, "secret-value")
	if err != nil {
		t.Fatalf("encryptCredentialValue error=%v", err)
	}
	decoded, err := decryptCredentialValue(key, encoded)
	if err != nil {
		t.Fatalf("decryptCredentialValue error=%v", err)
	}
	if decoded != "secret-value" {
		t.Fatalf("decoded=%q want %q", decoded, "secret-value")
	}
}

func TestMaskUsername(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "emmanuel@example.com", want: "e******l@example.com"},
		{input: "betuser", want: "b*****r"},
		{input: "ab", want: "a*"},
		{input: "a", want: "*"},
	}
	for _, tc := range tests {
		if got := maskUsername(tc.input); got != tc.want {
			t.Fatalf("maskUsername(%q)=%q want %q", tc.input, got, tc.want)
		}
	}
}
