package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthCallbackAllowed(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body authRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.Filters["ns"] != "prod" {
			t.Errorf("expected filter ns=prod, got %v", body.Filters)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected forwarded auth header")
		}
		json.NewEncoder(w).Encode(authResponse{Allowed: true})
	}))
	defer authServer.Close()

	ac := &AuthConfig{URL: authServer.URL, Timeout: 5_000_000_000}
	filters := map[string]string{"ns": "prod"}
	fakeReq, _ := http.NewRequest("GET", "/ws", nil)
	fakeReq.Header.Set("Authorization", "Bearer test-token")

	allowed, reason := ac.Check(fakeReq, filters, "all")
	if !allowed {
		t.Errorf("expected allowed, got denied: %s", reason)
	}
}

func TestAuthCallbackDenied(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(authResponse{Allowed: false, Reason: "no access"})
	}))
	defer authServer.Close()

	ac := &AuthConfig{URL: authServer.URL, Timeout: 5_000_000_000}
	fakeReq, _ := http.NewRequest("GET", "/ws", nil)

	allowed, reason := ac.Check(fakeReq, nil, "all")
	if allowed {
		t.Error("expected denied")
	}
	if reason != "no access" {
		t.Errorf("expected reason 'no access', got %q", reason)
	}
}

func TestAuthCallbackServerError(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer authServer.Close()

	ac := &AuthConfig{URL: authServer.URL, Timeout: 5_000_000_000}
	fakeReq, _ := http.NewRequest("GET", "/ws", nil)

	allowed, _ := ac.Check(fakeReq, nil, "all")
	if allowed {
		t.Error("expected denied on server error (fail closed)")
	}
}

func TestAuthCallbackTokenQueryParam(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer my-jwt" {
			t.Errorf("expected Authorization 'Bearer my-jwt', got %q", got)
		}
		json.NewEncoder(w).Encode(authResponse{Allowed: true})
	}))
	defer authServer.Close()

	ac := &AuthConfig{URL: authServer.URL, Timeout: 5_000_000_000}
	// Simulate browser WS upgrade: no Authorization header, token in query param
	fakeReq, _ := http.NewRequest("GET", "/ws?token=my-jwt&filter=ns:prod", nil)

	allowed, reason := ac.Check(fakeReq, map[string]string{"ns": "prod"}, "all")
	if !allowed {
		t.Errorf("expected allowed with token query param, got denied: %s", reason)
	}
}

func TestAuthCallbackNilConfig(t *testing.T) {
	var ac *AuthConfig
	fakeReq, _ := http.NewRequest("GET", "/ws", nil)
	allowed, _ := ac.Check(fakeReq, nil, "all")
	if !allowed {
		t.Error("expected allowed when auth is not configured")
	}
}
