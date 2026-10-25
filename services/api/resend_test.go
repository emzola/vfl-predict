package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendMailerSendsWithoutLoggingProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer re_test_key" {
			t.Fatalf("authorization=%q", got)
		}
		var body struct {
			From string   `json:"from"`
			To   []string `json:"to"`
			HTML string   `json:"html"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.From != "VFL <no-reply@example.test>" || len(body.To) != 1 || body.To[0] != "owner@example.test" {
			t.Fatalf("email envelope=%+v", body)
		}
		if !strings.Contains(body.HTML, "https://app.example.test/api/v1/auth/verify") {
			t.Fatalf("email body does not contain link: %q", body.HTML)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	mailer := resendMailer{url: server.URL + "/emails", apiKey: "re_test_key", from: "VFL <no-reply@example.test>", http: server.Client()}
	if err := mailer.SendMagicLink(context.Background(), "owner@example.test", "https://app.example.test/api/v1/auth/verify?token=opaque"); err != nil {
		t.Fatal(err)
	}
}

func TestProductionAuthConfigRequiresResendAndHTTPS(t *testing.T) {
	if err := ValidateProductionAuthConfig(AuthConfig{AuthorizedEmail: "owner@example.test", BaseURL: "http://example.test", CookieSecure: false}); err == nil {
		t.Fatal("incomplete production auth config unexpectedly passed")
	}
	if err := ValidateProductionAuthConfig(AuthConfig{
		AuthorizedEmail: "owner@example.test",
		BaseURL:         "https://example.test",
		CookieSecure:    true,
		Mailer:          resendMailer{apiKey: "key", from: "VFL <no-reply@example.test>"},
	}); err != nil {
		t.Fatal(err)
	}
}
