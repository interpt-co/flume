package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// AuthConfig configures the optional auth callback for WebSocket upgrades.
type AuthConfig struct {
	URL     string
	Timeout time.Duration
}

type authRequest struct {
	Filters map[string]string `json:"filters"`
	Pattern string            `json:"pattern"`
}

type authResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// Check calls the auth callback. Returns (true, "") if allowed or if auth is not configured.
// Fails closed on errors.
func (ac *AuthConfig) Check(r *http.Request, filters map[string]string, pattern string) (bool, string) {
	if ac == nil || ac.URL == "" {
		return true, ""
	}

	body, _ := json.Marshal(authRequest{
		Filters: filters,
		Pattern: pattern,
	})

	timeout := ac.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("POST", ac.URL, bytes.NewReader(body))
	if err != nil {
		return false, "auth request failed"
	}
	req.Header.Set("Content-Type", "application/json")

	if v := r.Header.Get("Authorization"); v != "" {
		req.Header.Set("Authorization", v)
	}
	if v := r.Header.Get("Cookie"); v != "" {
		req.Header.Set("Cookie", v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, "auth service unreachable"
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false, "unauthorized"
	}
	if resp.StatusCode != http.StatusOK {
		return false, "auth service error"
	}

	var result authResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&result); err != nil {
		return false, "invalid auth response"
	}

	if !result.Allowed {
		reason := result.Reason
		if reason == "" {
			reason = "access denied"
		}
		return false, reason
	}

	return true, ""
}
