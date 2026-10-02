package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

// CC Switch's Codex config, as it writes it: its "custom" table, one of
// its older versions' named by a profile, and a table of the user's own.
const ccSwitchCodex = "model_provider = \"custom\"\nmodel = \"gpt-5.4\"\nmodel_reasoning_effort = \"high\"\n\n" +
	"[model_providers.custom]\nname = \"custom\"\nbase_url = \"https://relay.example/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = true\nexperimental_bearer_token = \"sk-relay\"\n\n" +
	"[model_providers.cc-switch-2]\nname = \"old\"\nbase_url = \"https://old.example/v1\"\n\n" +
	"[model_providers.mine]\nname = \"mine\"\nbase_url = \"https://mine.example/v1\"\n\n" +
	"[profiles.work]\nmodel_provider = \"cc-switch-2\"\n"

// A Codex thread keeps the provider it was started on, and CC Switch puts
// every third-party one on its "custom" table: reopened after magpie took
// Codex over, it still went to the relay, a magpie model picked in it
// too, and the relay said the model wasn't found. While a magpie model is
// on, CC Switch's tables go through magpie too, and get their own base URL
// back when Codex steps off magpie; a profile's table and the user's own
// stay as they are.
func TestCodexTakesCCSwitchTables(t *testing.T) {
	for _, auth := range []string{`{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, ""} {
		home, read := codexHome(t, auth, ccSwitchCodex)
		path := filepath.Join(home, ".codex", "config.toml")
		cx := codex(home)
		table := func(id string) map[string]string {
			tb, err := edit.GetTOMLTable(path, "model_providers."+id)
			if err != nil {
				t.Fatal(err)
			}
			return tb
		}
		v1 := here(home).v1()
		if err := cx.Fields[0].Set("fake/m1"); err != nil {
			t.Fatal(err)
		}
		if c := table("custom"); c["base_url"] != v1 || c["experimental_bearer_token"] != "sk-relay" || c["requires_openai_auth"] != "true" {
			t.Fatalf("auth %q: custom not through magpie:\n%s", auth, read())
		}
		if table("cc-switch-2")["base_url"] != "https://old.example/v1" || table("mine")["base_url"] != "https://mine.example/v1" {
			t.Fatalf("auth %q: other tables changed:\n%s", auth, read())
		}
		if d := cx.Check(); d != "" {
			t.Fatalf("auth %q: check: %s", auth, d)
		}
		// CC Switch writes its table again: the row says so, and magpie's
		// next sync takes it over again
		if err := edit.SetTOMLKey(path, "model_providers.custom", "base_url", "https://relay2.example/v1"); err != nil {
			t.Fatal(err)
		}
		if d := cx.Check(); !strings.Contains(d, "[model_providers.custom]") || !strings.Contains(d, "relay2.example") {
			t.Fatalf("auth %q: check: %q", auth, d)
		}
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if table("custom")["base_url"] != v1 || cx.Check() != "" {
			t.Fatalf("auth %q: sync:\n%s", auth, read())
		}
		// one of Codex's own models: the table's own base URL is back
		if err := cx.Fields[0].Set("gpt-5.4"); err != nil {
			t.Fatal(err)
		}
		if table("custom")["base_url"] != "https://relay2.example/v1" {
			t.Fatalf("auth %q: own model:\n%s", auth, read())
		}
		// and a reset gives it back too
		if err := cx.Fields[0].Set("fake/m1"); err != nil {
			t.Fatal(err)
		}
		if err := cx.Fields[0].Set(""); err != nil {
			t.Fatal(err)
		}
		if table("custom")["base_url"] != "https://relay2.example/v1" {
			t.Fatalf("auth %q: reset:\n%s", auth, read())
		}
		if _, ok := stashLoad()[here(home).key("codex.tables")]; ok {
			t.Errorf("auth %q: stash left", auth)
		}
	}
}

// A CC Switch table the user pointed at magpie by hand stays so when Codex
// steps off magpie: magpie gives back only what it took.
func TestCodexKeepsHandPointedCCSwitchTable(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
	v1 := here(home).v1()
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"),
		[]byte("[model_providers.custom]\nname = \"custom\"\nbase_url = \""+v1+"\"\nwire_api = \"responses\"\n"), 0o644)
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Fields[0].Set(""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(), `base_url = "`+v1+`"`) {
		t.Fatalf("\n%s", read())
	}
}

// CC Switch's official OpenAI provider mirror writes model_provider = "custom"
// and a [model_providers.custom] table with name = "OpenAI", requires_openai_auth = true
// and no base_url (#504): it is the official provider, so its models are grouped
// under OpenAI and failover moves Codex onto magpie while another account is on.
func TestCodexCCSwitchOfficialOpenAIMirror(t *testing.T) {
	const ccSwitchOfficialMirror = "model_provider = \"custom\"\nmodel = \"gpt-5.4\"\n\n" +
		"[model_providers.custom]\nname = \"OpenAI\"\nrequires_openai_auth = true\nsupports_websockets = true\nwire_api = \"responses\"\n"

	// 1. Grouped under OpenAI, while a real relay is grouped under its provider id
	home, _ := codexHome(t, "", ccSwitchOfficialMirror)
	writeCache := func(h string) {
		dir := filepath.Join(h, ".codex")
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(`{"models":[
			{"slug":"gpt-5.4","display_name":"5.4","priority":1},
			{"slug":"gpt-5.5","display_name":"5.5","priority":2}]}`), 0o644)
	}
	writeCache(home)
	cx := codex(home)
	opts := cx.Fields[0].Options(nil)
	if len(opts) == 0 || opts[0].Group != "OpenAI" {
		t.Fatalf("mirror grouped under %q, want OpenAI", opts[0].Group)
	}

	homeRelay, _ := codexHome(t, "", ccSwitchCodex)
	writeCache(homeRelay)
	cxRelay := codex(homeRelay)
	optsRelay := cxRelay.Fields[0].Options(nil)
	if len(optsRelay) == 0 || optsRelay[0].Group != "custom" {
		t.Fatalf("relay grouped under %q, want custom", optsRelay[0].Group)
	}

	// 2. Account failover works on the official mirror
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth := func(email, acct string) map[string]any {
		return map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
			"id_token":      claims(map[string]any{"email": email}),
			"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
			"refresh_token": "r-" + acct, "account_id": acct}}
	}
	me, _ := json.Marshal(auth("me@example.com", "acct-1"))
	homeFailover, readFailover := codexHome(t, string(me), ccSwitchOfficialMirror)
	cxFailover := codex(homeFailover)
	logins := func(on bool) {
		b, _ := json.Marshal([]map[string]any{{"agent": "codex", "user": "spare@example.com", "on": on,
			"seen": time.Now(), "auth": auth("spare@example.com", "acct-2")}})
		os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
		os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), b, 0o600)
	}
	logins(true)
	if err := cxFailover.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := readFailover(); !strings.Contains(cfg, `openai_base_url = "http://127.0.0.1:`) {
		t.Fatalf("failover on did not set base url:\n%s", cfg)
	}
	logins(false)
	if err := cxFailover.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := readFailover(); strings.Contains(cfg, "openai_base_url") {
		t.Fatalf("failover off left base url:\n%s", cfg)
	}

	// But a real relay with a base_url never gets pointed at magpie gateway for failover
	homeRelayFailover, readRelayFailover := codexHome(t, string(me), ccSwitchCodex)
	cxRelayFailover := codex(homeRelayFailover)
	logins(true)
	if err := cxRelayFailover.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := readRelayFailover(); strings.Contains(cfg, "openai_base_url") {
		t.Fatalf("real relay got openai_base_url:\n%s", cfg)
	}
}
