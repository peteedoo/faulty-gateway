package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, 2) // 10/sec, burst 2

	// First two requests should pass (burst capacity)
	if !rl.Allow("1.2.3.4") {
		t.Error("first request should be allowed")
	}
	if !rl.Allow("1.2.3.4") {
		t.Error("second request should be allowed")
	}

	// Third request should fail (burst exhausted)
	if rl.Allow("1.2.3.4") {
		t.Error("third request should be rate limited")
	}

	// Different IP should pass
	if !rl.Allow("5.6.7.8") {
		t.Error("different IP should be allowed")
	}

	// Wait for token refill
	time.Sleep(200 * time.Millisecond)
	if !rl.Allow("1.2.3.4") {
		t.Error("request after refill should be allowed")
	}
}

func TestGatewayRouting(t *testing.T) {
	// Create a mock backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend ok"))
	}))
	defer backend.Close()

	routes := []Route{
		{Prefix: "/api/test", Backend: backend.URL},
	}
	gateway := NewGateway(routes)

	// Test matching route
	req := httptest.NewRequest("GET", "/api/test/foo", nil)
	rr := httptest.NewRecorder()
	gateway.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
}

func TestGatewayHealth(t *testing.T) {
	// Create a mock healthy backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"healthy"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	routes := []Route{
		{Prefix: "/api/test", Backend: backend.URL},
	}
	gateway := NewGateway(routes)

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	gateway.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp HealthStatus
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status healthy, got %s", resp.Status)
	}
}

func TestGatewayRateLimit(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	routes := []Route{{Prefix: "/", Backend: backend.URL}}
	gateway := NewGateway(routes)
	gateway.limiter = NewRateLimiter(1000, 1) // 1 req burst for testing

	// First request passes
	req1 := httptest.NewRequest("GET", "/", nil)
	rr1 := httptest.NewRecorder()
	gateway.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Errorf("first request should pass, got %d", rr1.Code)
	}

	// Second request from same IP should be rate limited
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.RemoteAddr = req1.RemoteAddr
	rr2 := httptest.NewRecorder()
	gateway.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("second request should be rate limited, got %d", rr2.Code)
	}
}
