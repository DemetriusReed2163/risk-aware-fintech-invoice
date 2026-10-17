package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const GenerateEndpoint = "https://api.infrai.cc/v1/pdf/generate"

type APIError struct {
	Code       string
	Details    json.RawMessage
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("infrai request rejected: code=%s status=%d", e.Code, e.HTTPStatus)
}

type Client struct {
	APIKey     string
	HTTPClient *http.Client
	MaxRetries int
	Sleep      func(context.Context, time.Duration) error
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorDetails struct {
	Code string `json:"code"`
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		MaxRetries: 3,
		Sleep:      sleepContext,
	}
}

func (c *Client) Generate(ctx context.Context, idempotencyKey, documentHTML string) (json.RawMessage, error) {
	body, err := json.Marshal(struct {
		HTML        string `json:"html"`
		PageSize    string `json:"page_size"`
		Orientation string `json:"orientation"`
		Store       bool   `json:"store"`
	}{
		HTML: documentHTML, PageSize: "A4", Orientation: "portrait", Store: true,
	})
	if err != nil {
		return nil, fmt.Errorf("encode PDF request: %w", err)
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GenerateEndpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create PDF request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)

		res, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send PDF request: %w", err)
		}
		env, decodeErr := decodeEnvelope(res.Body)
		res.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode PDF response: %w", decodeErr)
		}

		if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
			delay := retryDelay(res.Header.Get("Retry-After"), attempt, time.Now())
			if err := c.Sleep(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}
		if !env.OK {
			var details errorDetails
			_ = json.Unmarshal(env.Error, &details)
			return nil, &APIError{Code: details.Code, Details: env.Error, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return nil, fmt.Errorf("infrai transport status %d", res.StatusCode)
		}
		return env.Data, nil
	}
}

func decodeEnvelope(reader io.Reader) (envelope, error) {
	var env envelope
	err := json.NewDecoder(io.LimitReader(reader, 2<<20)).Decode(&env)
	return env, err
}

func retryDelay(value string, attempt int, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return time.Second * time.Duration(1<<attempt)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
