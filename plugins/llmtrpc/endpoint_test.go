package llmtrpc_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/plugins/llmtrpc"
)

// smokeServer answers one request with a minimal SSE body, so the live check runs
// against a real HTTP client and decoder with only the endpoint local.
func smokeServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(body))
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// sseText builds a minimal text answer as SSE.
func sseText(text string) string {
	return "data: {\"id\":\"c1\",\"model\":\"m-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" +
		text + "\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"c1\",\"model\":\"m-1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
}

// A live check against a loopback fixture passes and reports what it tried.
func TestSmokePassesAgainstALoopbackEndpoint(t *testing.T) {
	server := smokeServer(t, sseText("ready"))
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: server.URL + "/v1", Model: "m-1", APIKey: "fixture-key",
	}, fixedEnv(nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	result := llmtrpc.Smoke(context.Background(), llmtrpc.SmokeOptions{Endpoint: resolved, Timeout: 5 * time.Second})
	if result.Status != llmtrpc.SmokePassed {
		t.Fatalf("want passed, got %s (%s)", result.Status, result.Detail)
	}
	if !result.Status.Verifiable() {
		t.Fatal("a passed check must count as evidence")
	}
	if result.Duration <= 0 {
		t.Fatal("the result must report a duration")
	}
	// The report names the endpoint and model, and never the credential.
	description := result.Describe()
	if strings.Contains(description, "fixture-key") {
		t.Fatalf("the report leaked the credential: %q", description)
	}
	if !strings.Contains(description, "m-1") {
		t.Fatalf("the report must name the model: %q", description)
	}
}

// A missing credential is a skip, not a pass: the check produced no evidence.
func TestSmokeWithoutACredentialIsSkippedNotPassed(t *testing.T) {
	server := smokeServer(t, sseText("ready"))
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: server.URL + "/v1", Model: "m-1", AllowAnonymous: true,
	}, fixedEnv(nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	result := llmtrpc.Smoke(context.Background(), llmtrpc.SmokeOptions{Endpoint: resolved})
	if result.Status != llmtrpc.SmokeSkippedNoCredential {
		t.Fatalf("want skipped-no-credential, got %s", result.Status)
	}
	if result.Status.Verifiable() {
		t.Fatal("a skipped check must not count as evidence")
	}
	if result.Detail == "" {
		t.Fatal("a skip must explain itself")
	}
}

// An endpoint that cannot be reached is a failure, and the failure does not
// include the credential.
func TestSmokeReportsAnUnreachableEndpoint(t *testing.T) {
	// A closed port on loopback: the connection is refused immediately.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: url + "/v1", Model: "m-1", APIKey: "fixture-key",
	}, fixedEnv(nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	result := llmtrpc.Smoke(context.Background(), llmtrpc.SmokeOptions{Endpoint: resolved, Timeout: 5 * time.Second})
	if result.Status != llmtrpc.SmokeFailed {
		t.Fatalf("want failed, got %s", result.Status)
	}
	if strings.Contains(result.Describe(), "fixture-key") {
		t.Fatal("a failure report must not leak the credential")
	}
}

// An endpoint that answers with no text is a failure: it proves the connection
// works but not that the pipeline produced anything.
func TestSmokeWithAnEmptyAnswerFails(t *testing.T) {
	server := smokeServer(t, "data: [DONE]\n\n")
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: server.URL + "/v1", Model: "m-1", APIKey: "fixture-key",
	}, fixedEnv(nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	result := llmtrpc.Smoke(context.Background(), llmtrpc.SmokeOptions{Endpoint: resolved, Timeout: 5 * time.Second})
	if result.Status != llmtrpc.SmokeFailed {
		t.Fatalf("want failed for an empty answer, got %s", result.Status)
	}
}

// The status vocabulary makes exactly one outcome count as evidence.
func TestSmokeStatusVocabulary(t *testing.T) {
	statuses := []llmtrpc.SmokeStatus{
		llmtrpc.SmokePassed, llmtrpc.SmokeSkippedNoCredential,
		llmtrpc.SmokeSkippedByRequest, llmtrpc.SmokeFailed,
	}
	verifiable := 0
	for _, status := range statuses {
		if status.Verifiable() {
			verifiable++
		}
	}
	if verifiable != 1 {
		t.Fatalf("exactly one status may count as evidence, got %d", verifiable)
	}
	if !llmtrpc.SmokePassed.Verifiable() {
		t.Fatal("passed must count as evidence")
	}
}

// The environment-backed configuration reports a skip when no credential is
// present, rather than an error.
func TestSmokeConfigFromEnvironment(t *testing.T) {
	options, status, err := llmtrpc.SmokeConfigFromEnvironment(fixedEnv(nil))
	if err != nil {
		t.Fatalf("a missing credential is not an error: %v", err)
	}
	if status != llmtrpc.SmokeSkippedNoCredential {
		t.Fatalf("want skipped-no-credential, got %s", status)
	}
	if options.Endpoint.APIKey != "" {
		t.Fatal("no credential must have been resolved")
	}
	// A complete environment resolves an endpoint.
	options, status, err = llmtrpc.SmokeConfigFromEnvironment(fixedEnv(map[string]string{
		llmtrpc.EnvAPIKey: "k", llmtrpc.EnvBaseURL: "http://127.0.0.1:9", llmtrpc.EnvModel: "m-1",
	}))
	if err != nil {
		t.Fatalf("a complete environment must resolve: %v", err)
	}
	if status != "" {
		t.Fatalf("a resolved configuration carries no status, got %q", status)
	}
	if options.Endpoint.Model != "m-1" {
		t.Fatalf("unexpected model %q", options.Endpoint.Model)
	}
	// The source is reported as explicit because this helper read the environment
	// itself and then handed the value in: by the time ResolveEndpoint sees it, the
	// provenance it can observe is "the caller supplied this".
	if options.Endpoint.CredentialSource != llmtrpc.SourceExplicit {
		t.Fatalf("unexpected credential source %q", options.Endpoint.CredentialSource)
	}
	// A credential with a malformed endpoint is a failure the caller can record.
	if _, status, err = llmtrpc.SmokeConfigFromEnvironment(fixedEnv(map[string]string{
		llmtrpc.EnvAPIKey: "k", llmtrpc.EnvBaseURL: "ftp://example.com", llmtrpc.EnvModel: "m-1",
	})); !errors.Is(err, llmtrpc.ErrInvalidEndpoint) {
		t.Fatalf("want ErrInvalidEndpoint, got %v", err)
	}
	if status != llmtrpc.SmokeFailed {
		t.Fatalf("want failed for a malformed endpoint, got %s", status)
	}
}

// The check must not require a network when the credential is absent: a
// credential-free run returns before any request is attempted.
func TestSmokeWithoutACredentialMakesNoRequest(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer server.Close()
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: server.URL, Model: "m-1", AllowAnonymous: true,
	}, fixedEnv(nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	result := llmtrpc.Smoke(context.Background(), llmtrpc.SmokeOptions{Endpoint: resolved})
	if result.Status != llmtrpc.SmokeSkippedNoCredential {
		t.Fatalf("want skipped, got %s", result.Status)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("a credential-free check must not contact the endpoint")
	}
	// The environment the test runs in must not accidentally supply a credential.
	if value, ok := os.LookupEnv(llmtrpc.EnvAPIKey); ok && strings.TrimSpace(value) != "" {
		t.Skipf("the ambient environment supplies %s; the skip path is still asserted above", llmtrpc.EnvAPIKey)
	}
}
