package logincode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/cli-login-sms-codes/internal/infrai"
)

// Each case is one answer the API can give to a typed-in code, and the state a
// login attempt must land in because of it.
func TestCheckClassifiesAttempts(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   Outcome
		wantOK int
	}{
		{"correct code", 200, `{"ok":true,"data":{}}`, Verified, http.StatusOK},
		{"wrong code", 400, `{"ok":false,"error":{"code":"INVALID_ARGUMENT"}}`, Rejected, http.StatusUnauthorized},
		{"too many attempts", 429, `{"ok":false,"error":{"code":"RATE_LIMITED"}}`, Throttled, http.StatusTooManyRequests},
		{"upstream had nothing to say", 500, `{"ok":false,"error":{"code":"UNKNOWN"}}`, Failed, http.StatusBadGateway},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v1/sms/verify" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			svc := &Service{API: testClient(srv.URL)}
			got, err := svc.Check(context.Background(), "+15551230000", "314159")
			if err != nil {
				t.Fatalf("Check returned a transport error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("outcome = %q, want %q", got, tc.want)
			}
			if got.HTTPStatus() != tc.wantOK {
				t.Fatalf("status = %d, want %d", got.HTTPStatus(), tc.wantOK)
			}
		})
	}
}

// A retried Issue call must carry the same idempotency key it was given.
func TestIssueSendsIdempotencyKey(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Idempotency-Key")
		w.Write([]byte(`{"ok":true,"data":{"message_id":"sms_42"}}`))
	}))
	defer srv.Close()

	svc := &Service{API: testClient(srv.URL)}
	ch, err := svc.Issue(context.Background(), "+15551230000", "login-abc")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if ch.MessageID != "sms_42" {
		t.Fatalf("message id = %q, want sms_42", ch.MessageID)
	}
	if seen != "login-abc" {
		t.Fatalf("Idempotency-Key = %q, want login-abc", seen)
	}
}

func testClient(base string) *infrai.Client {
	return &infrai.Client{
		BaseURL:    base,
		Key:        "test-key",
		HTTP:       srvClient(),
		MaxRetries: 1,
		Sleep:      func(time.Duration) {},
	}
}

func srvClient() *http.Client { return &http.Client{Timeout: 5 * time.Second} }
