package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gosomea/deepseek-harness-go/plugins/llmtrpc"
)

// sessionProvider builds the model provider a session run uses.
//
// It returns nil when no endpoint is configured, and driveTurn then reports that
// a provider is required. nil is deliberate over a placeholder that always
// fails: the caller must state which configuration is missing rather than
// pretending a request happened.
//
// The environment is read through llmtrpc's own variables so the operator and
// the library agree on one spelling. The real endpoint is used only when a
// credential is present; a credential-free run reports the missing configuration
// instead of contacting anything.
func sessionProvider() Provider {
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: os.Getenv(llmtrpc.EnvBaseURL),
		APIKey:  os.Getenv(llmtrpc.EnvAPIKey),
		Model:   os.Getenv(llmtrpc.EnvModel),
	}, llmtrpc.OSEnvironment)
	if err != nil {
		// No usable endpoint: the run reports this rather than inventing a
		// response. The reason is not printed here because driveTurn owns the
		// diagnostic, and a second copy would be noise.
		return nil
	}
	if strings.TrimSpace(resolved.APIKey) == "" {
		return nil
	}
	return llmtrpc.New(llmtrpc.FromResolved(resolved, "local", "configured endpoint"))
}

// endpointDescription reports the configured endpoint for diagnostics, with the
// credential removed.
//
// It exists so a caller can print what it is about to use without ever holding
// the secret in a message.
func endpointDescription() string {
	resolved, err := llmtrpc.ResolveEndpoint(llmtrpc.EndpointConfig{
		BaseURL: os.Getenv(llmtrpc.EnvBaseURL),
		APIKey:  os.Getenv(llmtrpc.EnvAPIKey),
		Model:   os.Getenv(llmtrpc.EnvModel),
	}, llmtrpc.OSEnvironment)
	if err != nil {
		return fmt.Sprintf("endpoint=%s", llmtrpc.Redact(os.Getenv(llmtrpc.EnvBaseURL)))
	}
	return resolved.Describe()
}
