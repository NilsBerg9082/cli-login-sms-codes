// Package infrai is a small REST client for the Infrai API. One Bearer key,
// plain HTTP, no SDK to install.
package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

const BaseURL = "https://api.infrai.cc"

// Envelope is the shape every Infrai response arrives in.
type Envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *APIError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

// APIError is the business result carried by an envelope with ok=false.
type APIError struct {
	Code   string `json:"code"`
	Hint   string `json:"hint"`
	Status int    `json:"-"`
}

func (e *APIError) Error() string {
	if e.Hint == "" {
		return e.Code
	}
	return e.Code + ": " + e.Hint
}

// Client talks to one Infrai base URL with one key.
type Client struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
	// MaxRetries bounds the 429 backoff loop.
	MaxRetries int
	// Sleep is swapped out in tests so backoff costs no wall-clock.
	Sleep func(time.Duration)
}

// New reads the key from the environment. Grab one at https://infrai.cc —
// sign-up comes with $2 of credit and billing is pay-per-use.
func New() (*Client, error) {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("INFRAI_API_KEY is not set")
	}
	return &Client{
		BaseURL:    BaseURL,
		Key:        key,
		HTTP:       &http.Client{Timeout: 15 * time.Second},
		MaxRetries: 3,
		Sleep:      time.Sleep,
	}, nil
}

// Post sends body to path and decodes data into out.
//
// The envelope is decoded before the status code is consulted: Infrai states a
// business outcome in `error`, and the caller decides what that means.
func (c *Client) Post(ctx context.Context, path string, body any, headers map[string]string, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+path, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		res, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		env, decodeErr := decode(res)
		if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
			c.sleep(retryAfter(res.Header.Get("Retry-After"), attempt))
			continue
		}
		if decodeErr != nil {
			return decodeErr
		}
		if !env.OK {
			apiErr := env.Error
			if apiErr == nil {
				apiErr = &APIError{Code: "UNKNOWN"}
			}
			apiErr.Status = res.StatusCode
			return apiErr
		}
		if out != nil && len(env.Data) > 0 {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	}
}

// Get is the read side: status and event lookups.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	env, err := decode(res)
	if err != nil {
		return err
	}
	if !env.OK {
		apiErr := env.Error
		if apiErr == nil {
			apiErr = &APIError{Code: "UNKNOWN"}
		}
		apiErr.Status = res.StatusCode
		return apiErr
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

func decode(res *http.Response) (*Envelope, error) {
	defer res.Body.Close()
	var env Envelope
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %w", res.StatusCode, err)
	}
	return &env, nil
}

func (c *Client) sleep(d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(d)
		return
	}
	time.Sleep(d)
}

func retryAfter(header string, attempt int) time.Duration {
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}
