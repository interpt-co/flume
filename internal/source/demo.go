package source

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

// DemoSource generates fake log messages at a configurable rate.
type DemoSource struct {
	rate int // messages per second
}

// NewDemoSource creates a DemoSource that produces the given number of
// messages per second. If rate <= 0, it defaults to 10.
func NewDemoSource(rate int) *DemoSource {
	return &DemoSource{rate: rate}
}

// Name returns "demo".
func (s *DemoSource) Name() string {
	return "demo"
}

// Start begins generating fake log messages on the returned channel.
// The channel is closed when the context is cancelled.
func (s *DemoSource) Start(ctx context.Context) (<-chan models.LogMessage, error) {
	rate := s.rate
	if rate <= 0 {
		rate = 10
	}

	interval := time.Second / time.Duration(rate)
	ch := make(chan models.LogMessage)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		rng := rand.New(rand.NewSource(time.Now().UnixNano()))

		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				msg := models.LogMessage{
					Content:   generateLogLine(rng, t),
					Source:    models.SourceDemo,
					Origin:    models.Origin{Name: "demo"},
					Timestamp: time.Now(),
				}
				select {
				case <-ctx.Done():
					return
				case ch <- msg:
				}
			}
		}
	}()

	return ch, nil
}

// --- message templates -------------------------------------------------------

var httpMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
var httpPaths = []string{
	"/api/users", "/api/orders", "/api/products", "/api/auth/login",
	"/api/health", "/api/search", "/api/notifications", "/api/settings",
}
var httpStatuses = []int{200, 201, 204, 301, 400, 401, 403, 404, 500, 502, 503}
var levels = []string{"DEBUG", "INFO", "WARN", "ERROR"}
var services = []string{"http", "cache", "db", "auth", "queue", "scheduler", "worker"}

var plainTemplates = []func(*rand.Rand, time.Time) string{
	// HTTP request log
	func(rng *rand.Rand, t time.Time) string {
		method := httpMethods[rng.Intn(len(httpMethods))]
		path := httpPaths[rng.Intn(len(httpPaths))]
		status := httpStatuses[rng.Intn(len(httpStatuses))]
		latency := rng.Intn(500) + 1
		return fmt.Sprintf("%s INFO  [http] %s %s %d %dms",
			t.UTC().Format(time.RFC3339), method, path, status, latency)
	},
	// Cache hit/miss
	func(rng *rand.Rand, t time.Time) string {
		op := "hit"
		if rng.Intn(4) == 0 {
			op = "miss"
		}
		key := fmt.Sprintf("user:%d", rng.Intn(10000))
		ttl := (rng.Intn(6) + 1) * 60
		return fmt.Sprintf("%s DEBUG [cache] %s key=%s ttl=%ds",
			t.UTC().Format(time.RFC3339), op, key, ttl)
	},
	// Database query
	func(rng *rand.Rand, t time.Time) string {
		tables := []string{"users", "orders", "products", "sessions", "events"}
		table := tables[rng.Intn(len(tables))]
		rows := rng.Intn(1000)
		ms := rng.Intn(200) + 1
		return fmt.Sprintf("%s DEBUG [db] SELECT * FROM %s — %d rows in %dms",
			t.UTC().Format(time.RFC3339), table, rows, ms)
	},
	// Warning
	func(rng *rand.Rand, _ time.Time) string {
		pct := 70 + rng.Intn(26)
		mounts := []string{"/data", "/var/log", "/tmp", "/home"}
		mount := mounts[rng.Intn(len(mounts))]
		return fmt.Sprintf("[WARN] disk usage at %d%% on %s", pct, mount)
	},
	// Auth event
	func(rng *rand.Rand, t time.Time) string {
		users := []string{"alice", "bob", "charlie", "admin", "service-account"}
		user := users[rng.Intn(len(users))]
		actions := []string{"login succeeded", "login failed", "token refreshed", "session expired"}
		action := actions[rng.Intn(len(actions))]
		return fmt.Sprintf("%s INFO  [auth] user=%s %s",
			t.UTC().Format(time.RFC3339), user, action)
	},
	// Error
	func(rng *rand.Rand, t time.Time) string {
		errors := []string{
			"connection refused", "timeout after 30s", "TLS handshake failed",
			"too many open files", "out of memory", "deadlock detected",
		}
		svc := services[rng.Intn(len(services))]
		e := errors[rng.Intn(len(errors))]
		return fmt.Sprintf("%s ERROR [%s] %s",
			t.UTC().Format(time.RFC3339), svc, e)
	},
}

var jsonTemplates = []func(*rand.Rand, time.Time) string{
	// JSON HTTP log
	func(rng *rand.Rand, _ time.Time) string {
		method := httpMethods[rng.Intn(len(httpMethods))]
		path := httpPaths[rng.Intn(len(httpPaths))]
		status := httpStatuses[rng.Intn(len(httpStatuses))]
		dur := rng.Intn(500) + 1
		level := "info"
		if status >= 500 {
			level = "error"
		} else if status >= 400 {
			level = "warn"
		}
		return fmt.Sprintf(`{"level":"%s","method":"%s","path":"%s","status":%d,"duration":"%dms"}`,
			level, method, path, status, dur)
	},
	// JSON error
	func(rng *rand.Rand, _ time.Time) string {
		errors := []string{"connection refused", "timeout exceeded", "permission denied", "resource exhausted"}
		svc := services[rng.Intn(len(services))]
		e := errors[rng.Intn(len(errors))]
		latency := rng.Intn(10000) + 100
		return fmt.Sprintf(`{"level":"error","msg":"%s","service":"%s","latency_ms":%d}`,
			e, svc, latency)
	},
	// JSON info
	func(rng *rand.Rand, _ time.Time) string {
		events := []string{"job_completed", "backup_started", "config_reloaded", "health_check_ok"}
		event := events[rng.Intn(len(events))]
		svc := services[rng.Intn(len(services))]
		return fmt.Sprintf(`{"level":"info","event":"%s","service":"%s","count":%d}`,
			event, svc, rng.Intn(500))
	},
	// JSON debug
	func(rng *rand.Rand, _ time.Time) string {
		keys := []string{"user:123", "session:abc", "config:main", "rate:limit"}
		key := keys[rng.Intn(len(keys))]
		op := "GET"
		if rng.Intn(3) == 0 {
			op = "SET"
		}
		return fmt.Sprintf(`{"level":"debug","msg":"cache %s","key":"%s","ttl":%d}`,
			op, key, rng.Intn(3600))
	},
}

// generateLogLine picks a random template and fills it in.
func generateLogLine(rng *rand.Rand, t time.Time) string {
	// 40% chance of JSON, 60% plain text
	if rng.Intn(5) < 2 {
		tmpl := jsonTemplates[rng.Intn(len(jsonTemplates))]
		return tmpl(rng, t)
	}
	tmpl := plainTemplates[rng.Intn(len(plainTemplates))]
	return tmpl(rng, t)
}
