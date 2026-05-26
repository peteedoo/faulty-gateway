// Package main implements a lightweight reverse proxy with rate limiting
// and health check aggregation for the Faulty Link stack.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Route maps a path prefix to a backend URL.
type Route struct {
	Prefix  string
	Backend string
}

// Default routes for the Faulty Link stack.
var defaultRoutes = []Route{
	{Prefix: "/api/bridge", Backend: "http://localhost:8080"},
	{Prefix: "/api/dashboard", Backend: "http://localhost:3336"},
	{Prefix: "/api/brief", Backend: "http://localhost:3337"},
	{Prefix: "/", Backend: "http://localhost:3337"}, // default to daily-brief
}

// RateLimiter implements a token bucket per client IP.
type RateLimiter struct {
	mu       sync.RWMutex
	buckets  map[string]*bucket
	rate     float64 // tokens per second
	capacity float64 // max tokens
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewRateLimiter creates a rate limiter with the given rate and capacity.
func NewRateLimiter(rate, capacity float64) *RateLimiter {
	return &RateLimiter{
		buckets:  make(map[string]*bucket),
		rate:     rate,
		capacity: capacity,
	}
}

// Allow returns true if the client IP is within rate limits.
func (rl *RateLimiter) Allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[clientIP]
	if !ok {
		b = &bucket{tokens: rl.capacity, lastCheck: time.Now()}
		rl.buckets[clientIP] = b
	}

	now := time.Now()
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens = min(b.tokens+elapsed*rl.rate, rl.capacity)
	b.lastCheck = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// HealthStatus aggregates health from all backends.
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Services  map[string]string `json:"services"`
}

// Gateway is the reverse proxy with rate limiting and health checks.
type Gateway struct {
	routes     []Route
	proxies    map[string]*httputil.ReverseProxy
	limiter    *RateLimiter
	healthPath string
}

// NewGateway creates a new gateway with the given routes.
func NewGateway(routes []Route) *Gateway {
	g := &Gateway{
		routes:     routes,
		proxies:    make(map[string]*httputil.ReverseProxy),
		limiter:    NewRateLimiter(10, 20), // 10 req/s, burst 20
		healthPath: "/health",
	}

	for _, r := range routes {
		target, err := url.Parse(r.Backend)
		if err != nil {
			log.Printf("Invalid backend URL %s: %v", r.Backend, err)
			continue
		}
		g.proxies[r.Prefix] = httputil.NewSingleHostReverseProxy(target)
	}

	return g
}

// ServeHTTP implements the http.Handler interface.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract client IP
	clientIP := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		clientIP = strings.Split(fwd, ",")[0]
	}

	// Rate limit check (skip for health endpoint)
	if r.URL.Path != g.healthPath && !g.limiter.Allow(clientIP) {
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
		return
	}

	// Health check endpoint
	if r.URL.Path == g.healthPath {
		g.handleHealth(w, r)
		return
	}

	// Find matching route
	for _, route := range g.routes {
		if strings.HasPrefix(r.URL.Path, route.Prefix) {
			proxy, ok := g.proxies[route.Prefix]
			if ok {
				// Strip prefix if it's not root
				if route.Prefix != "/" {
					r.URL.Path = strings.TrimPrefix(r.URL.Path, route.Prefix)
					if r.URL.Path == "" {
						r.URL.Path = "/"
					}
				}
				proxy.ServeHTTP(w, r)
				return
			}
		}
	}

	http.NotFound(w, r)
}

// handleHealth aggregates health from all backends.
func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now(),
		Services:  make(map[string]string),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	for _, route := range g.routes {
		if route.Prefix == "/" {
			continue // skip default route
		}
		backendURL := route.Backend + "/health"
		req, err := http.NewRequestWithContext(ctx, "GET", backendURL, nil)
		if err != nil {
			status.Services[route.Prefix] = "error: " + err.Error()
			status.Status = "degraded"
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			status.Services[route.Prefix] = "unreachable"
			status.Status = "degraded"
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			status.Services[route.Prefix] = "healthy"
		} else {
			status.Services[route.Prefix] = fmt.Sprintf("unhealthy (%d)", resp.StatusCode)
			status.Status = "degraded"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if status.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(status)
}

func main() {
	gateway := NewGateway(defaultRoutes)
	
	addr := ":8888"
	log.Printf("Faulty Gateway listening on %s", addr)
	log.Printf("Routes:")
	for _, r := range defaultRoutes {
		log.Printf("  %s -> %s", r.Prefix, r.Backend)
	}
	
	if err := http.ListenAndServe(addr, gateway); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
