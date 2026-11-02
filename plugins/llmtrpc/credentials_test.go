package llmtrpc_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/llmtrpc"
)

// fixedEnv builds a lookup over a fixed map, so a test never mutates the real
// process environment.
func fixedEnv(values map[string]string) llmtrpc.Lookup {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

// An explicit credential wins over the environment, so a deployment's behaviour
// does not depend on ambient state.
func TestExplicitCredentialWins(t *testing.T) {
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: "https://api.example.com", APIKey: "from-code", Model: "m-1",
	}, fixedEnv(map[string]string{llmtrpc.EnvAPIKey: "from-env"}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.APIKey != "from-code" {
		t.Fatalf("the explicit credential must win, got %q", resolved.APIKey)
	}
	if resolved.CredentialSource != llmtrpc.SourceExplicit {
		t.Fatalf("the source must be reported: %q", resolved.CredentialSource)
	}
}

// The environment is used when code supplies nothing.
func TestEnvironmentCredentialIsUsed(t *testing.T) {
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: "https://api.example.com", Model: "m-1",
	}, fixedEnv(map[string]string{llmtrpc.EnvAPIKey: "from-env", llmtrpc.EnvModel: "env-model"}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.APIKey != "from-env" {
		t.Fatalf("the environment credential must be used, got %q", resolved.APIKey)
	}
	if resolved.CredentialSource != llmtrpc.SourceEnvironment {
		t.Fatalf("the source must be reported: %q", resolved.CredentialSource)
	}
	if resolved.Model != "m-1" {
		t.Fatalf("an explicit model must beat the environment: %q", resolved.Model)
	}
}

// A remote endpoint without a credential is refused rather than attempted
// anonymously.
func TestRemoteEndpointWithoutCredentialIsRefused(t *testing.T) {
	_, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: "https://api.example.com", Model: "m-1",
	}, fixedEnv(nil))
	if !errors.Is(err, llmtrpc.ErrMissingCredential) {
		t.Fatalf("want ErrMissingCredential, got %v", err)
	}
	// A private-but-remote host is not loopback: being reachable on a local
	// network does not make an endpoint credential-free.
	if _, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: "http://192.168.1.10:8080", Model: "m-1",
	}, fixedEnv(nil)); !errors.Is(err, llmtrpc.ErrMissingCredential) {
		t.Fatalf("want ErrMissingCredential for a non-loopback host, got %v", err)
	}
}

// A loopback endpoint may be anonymous without an explicit flag, because that is
// the configuration a local fixture uses.
func TestLoopbackEndpointMayBeAnonymous(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:8080", "http://localhost:8080", "http://[::1]:8080"} {
		resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{BaseURL: base, Model: "m-1"}, fixedEnv(nil))
		if err != nil {
			t.Fatalf("%s must be usable anonymously: %v", base, err)
		}
		if resolved.CredentialSource != llmtrpc.SourceNone {
			t.Fatalf("a fixture endpoint has no credential, got %q", resolved.CredentialSource)
		}
	}
	// An explicit opt-in also permits it, which is what a caller who knows the
	// endpoint needs no credential states.
	if _, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: "https://internal.example.com", Model: "m-1", AllowAnonymous: true,
	}, fixedEnv(nil)); err != nil {
		t.Fatalf("an explicit anonymous endpoint must be allowed: %v", err)
	}
}

// Malformed endpoints are refused with a reason that names the problem.
func TestInvalidEndpointsAreRefused(t *testing.T) {
	cases := map[string]llmtrpc.EndpointConfig{
		"no url":      {Model: "m-1"},
		"bad scheme":  {BaseURL: "ftp://example.com", Model: "m-1"},
		"no scheme":   {BaseURL: "example.com", Model: "m-1"},
		"no host":     {BaseURL: "https://", Model: "m-1"},
		"no model":    {BaseURL: "https://api.example.com"},
		"unparseable": {BaseURL: "https://[::1", Model: "m-1"},
	}
	for name, config := range cases {
		if _, err := llmtrpc.ResolveEndpoint(config, fixedEnv(map[string]string{llmtrpc.EnvAPIKey: "k"})); !errors.Is(err, llmtrpc.ErrInvalidEndpoint) {
			t.Fatalf("%s: want ErrInvalidEndpoint, got %v", name, err)
		}
	}
	// The endpoint URL and the model can come from the environment.
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{}, fixedEnv(map[string]string{
		llmtrpc.EnvBaseURL: "http://127.0.0.1:9", llmtrpc.EnvModel: "env-model",
	}))
	if err != nil {
		t.Fatalf("resolve from the environment: %v", err)
	}
	if resolved.Model != "env-model" {
		t.Fatalf("unexpected model %q", resolved.Model)
	}
}

// Diagnostics never carry the credential: they report its source and length.
func TestDiagnosticsNeverCarryTheCredential(t *testing.T) {
	secret := "sk-super-secret-value-0123456789"
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: "https://api.example.com", APIKey: secret, Model: "m-1",
	}, fixedEnv(nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	description := resolved.Describe()
	if strings.Contains(description, secret) {
		t.Fatalf("the description leaked the credential: %q", description)
	}
	if !strings.Contains(description, "chars") {
		t.Fatalf("the description must report a length signal: %q", description)
	}
	if !strings.Contains(description, string(llmtrpc.SourceExplicit)) {
		t.Fatalf("the description must report the source: %q", description)
	}
	if resolved.Redacted() != "https://api.example.com" {
		t.Fatalf("an endpoint without userinfo is unchanged: %q", resolved.Redacted())
	}
	// A credential embedded in the URL's userinfo is removed from the display.
	redacted := llmtrpc.Redact("https://user:supersecret@api.example.com/v1")
	if strings.Contains(redacted, "supersecret") {
		t.Fatalf("Redact leaked the credential: %q", redacted)
	}
	if llmtrpc.Redact("https://[::1") != "<unparseable endpoint>" {
		t.Fatal("an unparseable endpoint must be reported as such, not echoed")
	}
	// A resolved endpoint whose URL is unparseable is reported safely too.
	broken := llmtrpc.ResolvedEndpoint{BaseURL: "https://[::1"}
	if strings.Contains(broken.Redacted(), "[::1") {
		t.Fatalf("an unparseable endpoint must not be echoed: %q", broken.Redacted())
	}
	if !strings.Contains(broken.Describe(), "unparseable") {
		t.Fatalf("the description must report the problem: %q", broken.Describe())
	}
}

// The resolved endpoint can be handed to the adapter without the credential being
// copied anywhere it should not be.
func TestFromResolvedBuildsAnAdapterConfig(t *testing.T) {
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: "http://127.0.0.1:8080", Model: "m-1", APIKey: "k",
	}, fixedEnv(nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	config := llmtrpc.FromResolved(resolved, "route-1", "Local")
	if config.ID != "route-1" || config.Name != "Local" || config.Model != "m-1" || config.APIKey != "k" {
		t.Fatalf("unexpected adapter config %+v", config)
	}
	adapter := llmtrpc.New(config)
	if adapter == nil {
		t.Fatal("the adapter must be constructed")
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
