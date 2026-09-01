// Package logincode models the phone-code step of a developer-tools login:
// the CLI asks for a code, the user types it back, the daemon decides whether
// that attempt becomes a session.
package logincode

import (
	"context"
	"errors"
	"net/http"

	"github.com/example/cli-login-sms-codes/internal/infrai"
)

// Outcome is the state a verification attempt lands in. It is the value the
// CLI branches on, so it is deliberately small and closed.
type Outcome string

const (
	Verified  Outcome = "verified"  // code matched, hand out a session
	Rejected  Outcome = "rejected"  // wrong or expired code, let the user retype
	Throttled Outcome = "throttled" // too many attempts for this number
	Failed    Outcome = "failed"    // we could not reach a decision
)

// HTTPStatus is what the daemon returns to the CLI for this outcome.
func (o Outcome) HTTPStatus() int {
	switch o {
	case Verified:
		return http.StatusOK
	case Rejected:
		return http.StatusUnauthorized
	case Throttled:
		return http.StatusTooManyRequests
	default:
		return http.StatusBadGateway
	}
}

// Service issues and checks login codes.
type Service struct {
	API *infrai.Client
}

// Challenge is what the CLI needs in order to poll delivery.
type Challenge struct {
	MessageID string `json:"message_id"`
}

type sendReply struct {
	MessageID string `json:"message_id"`
}

// Issue texts a one-time code to phone and returns the message id.
// requestID rides along as an idempotency key, so a CLI that retries a dropped
// connection gets the same challenge back instead of a second text message.
func (s *Service) Issue(ctx context.Context, phone, requestID string) (Challenge, error) {
	var reply sendReply
	err := s.API.Post(ctx, "/v1/sms/otp",
		map[string]any{"to": phone},
		map[string]string{"Idempotency-Key": requestID},
		&reply)
	if err != nil {
		return Challenge{}, err
	}
	return Challenge{MessageID: reply.MessageID}, nil
}

// Check submits the code the user typed and classifies the answer.
// A refused code is a normal answer, not a transport failure: it comes back as
// Rejected with a nil error and the CLI reprompts.
func (s *Service) Check(ctx context.Context, phone, code string) (Outcome, error) {
	err := s.API.Post(ctx, "/v1/sms/verify",
		map[string]any{"to": phone, "code": code}, nil, nil)
	if err == nil {
		return Verified, nil
	}
	var apiErr *infrai.APIError
	if errors.As(err, &apiErr) {
		return classify(apiErr), nil
	}
	return Failed, err
}

// Delivery reports where the text message got to — the one diagnostic a
// support ticket always asks for.
func (s *Service) Delivery(ctx context.Context, messageID string) (map[string]any, error) {
	out := map[string]any{}
	if err := s.API.Get(ctx, "/v1/sms/status/"+messageID, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// classify turns an API answer into one of our four states. Anything we do not
// recognise stays Failed rather than being guessed into a login.
func classify(e *infrai.APIError) Outcome {
	switch {
	case e.Status == http.StatusTooManyRequests:
		return Throttled
	case e.Status >= 400 && e.Status < 500:
		return Rejected
	default:
		return Failed
	}
}
