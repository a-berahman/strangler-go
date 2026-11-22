package router

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-berahman/strangler-go/internal/customers"
	"github.com/a-berahman/strangler-go/internal/legacy"
)

// logBuf is a bytes.Buffer that is safe to write from many goroutines.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func setup(t *testing.T, legacyH, newH http.Handler, routes []Route, timeout time.Duration) (*httptest.Server, *Router, *logBuf) {
	t.Helper()
	old := httptest.NewServer(legacyH)
	t.Cleanup(old.Close)
	next := httptest.NewServer(newH)
	t.Cleanup(next.Close)

	logs := &logBuf{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rt, err := New(Config{
		Legacy:        old.URL,
		New:           next.URL,
		ShadowTimeout: Duration(timeout),
		Routes:        routes,
		Ignore:        []string{"$.legacy_rev", "$._flags"},
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(rt)
	t.Cleanup(front.Close)
	return front, rt, logs
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestModes(t *testing.T) {
	front, _, _ := setup(t, legacy.Handler(), customers.Handler(customers.NewStore()),
		[]Route{{Prefix: "/customers", Mode: ModeNew}, {Prefix: "/orders", Mode: ModeLegacy}}, time.Second)

	tests := []struct {
		path string
		want string
	}{
		{"/customers/c-1", "new"},
		{"/customers", "new"},
		{"/orders?customer=c-1", "legacy"},
		{"/customersfoo", "legacy"}, // not a real prefix match
	}
	for _, tt := range tests {
		resp, _ := get(t, front.URL+tt.path)
		if got := resp.Header.Get("X-Served-By"); got != tt.want {
			t.Errorf("%s: served by %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestShadowReturnsLegacyAndLogsMismatch(t *testing.T) {
	front, rt, logs := setup(t, legacy.Handler(), customers.Handler(customers.NewStore()),
		[]Route{{Prefix: "/customers", Mode: ModeShadow}}, time.Second)

	resp, body := get(t, front.URL+"/customers/c-2")
	rt.Wait()
	if resp.Header.Get("X-Served-By") != "legacy" || !strings.Contains(body, `"phones":null`) {
		t.Fatalf("client should get the legacy answer, got %s", body)
	}
	out := logs.String()
	if !strings.Contains(out, "shadow mismatch") || !strings.Contains(out, "$.phones: null vs empty") {
		t.Errorf("expected null vs empty mismatch in logs:\n%s", out)
	}

	// c-1 has a phone in both, and legacy_rev is ignored, so it should match.
	get(t, front.URL+"/customers/c-1")
	rt.Wait()
	if !strings.Contains(logs.String(), `msg="shadow match" method=GET path=/customers/c-1`) {
		t.Errorf("expected a match for c-1:\n%s", logs.String())
	}

	// Legacy says 200 with an error body, new says 404.
	get(t, front.URL+"/customers/c-404")
	rt.Wait()
	if !strings.Contains(logs.String(), "status: value changed (old=200 new=404)") {
		t.Errorf("expected status mismatch:\n%s", logs.String())
	}
}

func TestSlowNewServiceDoesNotSlowTheCaller(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	front, rt, logs := setup(t, legacy.Handler(), slow,
		[]Route{{Prefix: "/customers", Mode: ModeShadow}}, 50*time.Millisecond)

	start := time.Now()
	resp, _ := get(t, front.URL+"/customers/c-1")
	if took := time.Since(start); took > time.Second {
		t.Fatalf("real call took %s, shadow is blocking it", took)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	done := make(chan struct{})
	go func() { rt.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shadow goroutine is still running after its deadline")
	}
	if !strings.Contains(logs.String(), "shadow call failed") {
		t.Errorf("expected the timeout in logs:\n%s", logs.String())
	}
}

func TestWritesAreNotShadowed(t *testing.T) {
	var hits atomic.Int32
	counter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })
	front, rt, _ := setup(t, legacy.Handler(), counter,
		[]Route{{Prefix: "/orders", Mode: ModeShadow}}, time.Second)

	resp, err := http.Post(front.URL+"/orders", "application/json", strings.NewReader(`{"items":["book"]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	rt.Wait()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status %d, want 201 from legacy", resp.StatusCode)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("new service got %d POST calls, want 0", n)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	os.WriteFile(good, []byte(`{"legacy":"http://a","new":"http://b","shadow_timeout":"150ms",
		"routes":[{"prefix":"/customers","mode":"shadow"}]}`), 0o600)
	cfg, err := Load(good)
	if err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.ShadowTimeout) != 150*time.Millisecond || cfg.Listen != ":8080" {
		t.Errorf("unexpected config %+v", cfg)
	}

	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"legacy":"http://a","new":"http://b","routes":[{"prefix":"/x","mode":"maybe"}]}`), 0o600)
	if _, err := Load(bad); err == nil {
		t.Error("expected error for unknown mode")
	}
}
