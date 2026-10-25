package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/usage"
)

// A System One response that counts cache tokens (cache_read_input_tokens,
// cache_creation_input_tokens, TTL split) records them in usage.Record and
// in the route's trace (#3471).
func TestSystemOneUsageRecordsCacheTokens(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"jev-cache","answers":{"intent":{"type":"choice","choice":"bug"}},`+
			`"usage":{"input_tokens":120,"output_tokens":2,"cache_read_input_tokens":800,"cache_creation_input_tokens":300,`+
			`"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":200}}}`)
	}))
	defer up.Close()

	if err := provider.Save(provider.Provider{ID: "jev", Name: "Jev", Key: "secret", Decide: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}

	gw := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(`{"model":"jev/jev-latest","questions":{"intent":{"type":"choice"}}}`))
	req.Header.Set("Authorization", "Bearer magpie")
	gw.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}

	r := lastUsage(t)
	if r.Input != 120 || r.Output != 2 || r.CacheRead != 800 || r.CacheWrite != 300 || r.CacheWrite1h != 200 {
		t.Fatalf("usage record cache tokens: %+v", r)
	}

	if len(gw.trace.routes) != 1 {
		t.Fatalf("traced routes: %d", len(gw.trace.routes))
	}
	tr := gw.trace.routes[0]
	const wantTokens = 120 + 2 + 800 + 300
	if tr.Tokens != wantTokens {
		t.Fatalf("traced tokens %d, want %d", tr.Tokens, wantTokens)
	}
	if len(tr.Usage) != 1 {
		t.Fatalf("traced usage: %+v", tr.Usage)
	}
	u := tr.Usage[0]
	if u.Input != 120 || u.Output != 2 || u.CacheRead != 800 || u.CacheWrite != 300 || u.CacheUnknown {
		t.Fatalf("traced RouteUsage: %+v", u)
	}

	// router's own internal classification call
	p, err := provider.Find("jev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.systemOne(context.Background(), *p, "jev-latest", []byte(`{"questions":{}}`)); err != nil {
		t.Fatal(err)
	}
	own := lastUsage(t)
	if own.Agent != usage.AgentOf(RouterAgent) || own.Kind != "routing" {
		t.Fatalf("router usage attribution: %+v", own)
	}
	if own.Input != 120 || own.Output != 2 || own.CacheRead != 800 || own.CacheWrite != 300 || own.CacheWrite1h != 200 {
		t.Fatalf("router usage cache tokens: %+v", own)
	}
}

// A System One response that reports magpie_cache_unknown (e.g. from Kiro or
// a plugin that does not count cache tokens) sets CacheUnknown on RouteUsage
// without reporting a false miss.
func TestSystemOneUsageRecordsCacheUnknown(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"jev-unknown","answers":{},"usage":{"input_tokens":50,"output_tokens":3,"magpie_cache_unknown":true}}`)
	}))
	defer up.Close()

	if err := provider.Save(provider.Provider{ID: "jev-unk", Name: "Jev Unknown", Key: "secret", Decide: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}

	gw := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(`{"model":"jev-unk/jev-latest","questions":{}}`))
	req.Header.Set("Authorization", "Bearer magpie")
	gw.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}

	r := lastUsage(t)
	if r.Input != 50 || r.Output != 3 || r.CacheRead != 0 || r.CacheWrite != 0 {
		t.Fatalf("usage record: %+v", r)
	}

	if len(gw.trace.routes) != 1 {
		t.Fatalf("traced routes: %d", len(gw.trace.routes))
	}
	tr := gw.trace.routes[0]
	if len(tr.Usage) != 1 || !tr.Usage[0].CacheUnknown {
		t.Fatalf("expected CacheUnknown in RouteUsage: %+v", tr.Usage)
	}
}

// A decision group preserves cache tokens from whichever member answers.
func TestSystemOneDecisionGroupRecordsCacheTokens(t *testing.T) {
	fresh(t)
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"detail":{"message":"rate limited"}}`)
	}))
	defer upA.Close()

	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"jb-decide","answers":{"intent":{"type":"choice","choice":"bug"}},`+
			`"usage":{"input_tokens":90,"output_tokens":4,"cache_read_input_tokens":500,"cache_creation_input_tokens":150}}`)
	}))
	defer upB.Close()

	for _, p := range []provider.Provider{
		{ID: "ja", Name: "JA", Key: "ka", Decide: upA.URL + "/v1"},
		{ID: "jb", Name: "JB", Key: "kb", Decide: upB.URL + "/v1"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := provider.SaveGroup(provider.Group{ID: "jgroup", Name: "JGroup", Members: []string{"ja/m", "jb/m"}}); err != nil {
		t.Fatal(err)
	}

	gw := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(`{"model":"group/jgroup","questions":{"intent":{"type":"choice"}}}`))
	req.Header.Set("Authorization", "Bearer magpie")
	gw.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}

	r := lastUsage(t)
	if r.Provider != "jb" || r.Input != 90 || r.Output != 4 || r.CacheRead != 500 || r.CacheWrite != 150 {
		t.Fatalf("group usage record: %+v", r)
	}

	if len(gw.trace.routes) != 1 {
		t.Fatalf("traced routes: %d", len(gw.trace.routes))
	}
	tr := gw.trace.routes[0]
	const wantTokens = 90 + 4 + 500 + 150
	if tr.Tokens != wantTokens {
		t.Fatalf("traced group tokens %d, want %d", tr.Tokens, wantTokens)
	}
	if len(tr.Usage) != 1 || tr.Usage[0].CacheRead != 500 || tr.Usage[0].CacheWrite != 150 {
		t.Fatalf("traced group RouteUsage: %+v", tr.Usage)
	}
}
