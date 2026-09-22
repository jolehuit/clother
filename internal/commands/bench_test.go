package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func TestBenchModelFallsBackToTiers(t *testing.T) {
	t.Parallel()

	explicit := profiles.Target{Model: "glm-5.2", ModelTiers: map[string]string{"sonnet": "other"}}
	if got := benchModel(explicit); got != "glm-5.2" {
		t.Fatalf("benchModel(explicit) = %q, want glm-5.2", got)
	}

	// OpenRouter aliases resolve with an empty Model and tier-only mapping.
	alias := profiles.Target{ModelTiers: map[string]string{
		"haiku":  "minimax/minimax-m2.5:free",
		"sonnet": "minimax/minimax-m2.5:free",
	}}
	if got := benchModel(alias); got != "minimax/minimax-m2.5:free" {
		t.Fatalf("benchModel(alias) = %q, want the tier model", got)
	}

	empty := profiles.Target{}
	if got := benchModel(empty); got != "" {
		t.Fatalf("benchModel(empty) = %q, want empty", got)
	}
}

func TestDoBenchSendsTheLauncherCredentialAndStripsContextSuffix(t *testing.T) {
	t.Parallel()

	type seen struct{ model, apiKey, bearer string }
	requests := make(chan seen, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		requests <- seen{body.Model, r.Header.Get("x-api-key"), r.Header.Get("Authorization")}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	kimi := profiles.Target{
		Profile: "kimi", BaseURL: server.URL, Model: "k3[1m]",
		AuthMode: providers.AuthSecret, SecretKey: "KIMI_API_KEY", CredentialEnvVar: providers.APIKeyEnvVar,
	}
	res := doBench(context.Background(), kimi, config.Secrets{"KIMI_API_KEY": "sk-kimi"}, "hi")
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	got := <-requests
	if got.model != "k3" || got.apiKey != "sk-kimi" || got.bearer != "" {
		t.Fatalf("kimi request = %+v, want model k3 with x-api-key only", got)
	}

	zai := profiles.Target{
		Profile: "zai", BaseURL: server.URL, Model: "glm-5.3[1m]",
		AuthMode: providers.AuthSecret, SecretKey: "ZAI_API_KEY", CredentialEnvVar: providers.AuthTokenEnvVar,
	}
	if res := doBench(context.Background(), zai, config.Secrets{"ZAI_API_KEY": "sk-zai"}, "hi"); res.Err != nil {
		t.Fatal(res.Err)
	}
	got = <-requests
	if got.model != "glm-5.3" || got.bearer != "Bearer sk-zai" || got.apiKey != "" {
		t.Fatalf("zai request = %+v, want model glm-5.3 with a bearer token only", got)
	}
}
