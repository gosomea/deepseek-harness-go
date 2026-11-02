package llmtrpc

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ErrMissingCredential reports that a real endpoint was configured without a
// credential.
//
// It is distinct from a generic configuration error because the remedy differs:
// a missing credential is supplied by the operator, while a malformed endpoint is
// fixed by whoever wrote the configuration.
var ErrMissingCredential = errors.New("llmtrpc: the endpoint requires a credential")

// ErrInvalidEndpoint reports an endpoint that cannot be used.
var ErrInvalidEndpoint = errors.New("llmtrpc: invalid endpoint")

// Environment names this package reads. They are named constants so an operator
// and a test refer to the same string.
const (
	// EnvAPIKey is the credential variable.
	EnvAPIKey = "DSH_LLM_API_KEY"
	// EnvBaseURL is the endpoint variable.
	EnvBaseURL = "DSH_LLM_BASE_URL"
	// EnvModel is the model identifier variable.
	EnvModel = "DSH_LLM_MODEL"
)

// CredentialSource records where a resolved credential came from.
//
// It is reported rather than inferred because "which credential won" is the
// question an operator asks first when a request fails, and an answer that
// required reading the resolver would arrive too late.
type CredentialSource string

// The sources this package resolves, in precedence order.
const (
	// SourceExplicit is a value passed in code.
	SourceExplicit CredentialSource = "explicit"
	// SourceEnvironment is a value read from the process environment.
	SourceEnvironment CredentialSource = "environment"
	// SourceNone means no credential was found.
	SourceNone CredentialSource = "none"
)

// EndpointConfig is the raw configuration of one route.
type EndpointConfig struct {
	// BaseURL is the endpoint. Required.
	BaseURL string
	// APIKey is the credential; empty means "not supplied".
	APIKey string
	// Model is the model identifier. Required.
	Model string
	// AllowAnonymous permits an endpoint with no credential.
	//
	// It exists for a local loopback fixture, which legitimately needs no
	// credential. It is explicit rather than implied by an empty key, so a real
	// endpoint that forgot its credential cannot pass by omission.
	AllowAnonymous bool
}

// ResolvedEndpoint is an endpoint with its credential resolved.
type ResolvedEndpoint struct {
	// BaseURL is the validated endpoint URL.
	BaseURL string
	// APIKey is the resolved credential.
	APIKey string
	// Model is the model identifier.
	Model string
	// CredentialSource records where the credential came from.
	CredentialSource CredentialSource
}

// Lookup reads one environment variable.
//
// It is a function type so a test can supply a fixed environment instead of
// mutating the process's.
type Lookup func(name string) (string, bool)

// OSEnvironment reads the process environment.
func OSEnvironment(name string) (string, bool) { return os.LookupEnv(name) }

// ResolveEndpoint validates an endpoint and resolves its credential.
//
// Precedence is explicit configuration, then the environment, then none. The
// order matters: an operator's environment should not silently override a value
// the program stated in code, because that would make a deployment's behaviour
// depend on ambient state.
//
// A remote endpoint without a credential is refused. A loopback endpoint may be
// anonymous, but only when the caller said so explicitly — inferring that from an
// empty key would let a real endpoint missing its credential pass by omission.
func ResolveEndpoint(config EndpointConfig, lookup Lookup) (ResolvedEndpoint, error) {
	if lookup == nil {
		lookup = OSEnvironment
	}
	base := strings.TrimSpace(config.BaseURL)
	if base == "" {
		if fromEnv, ok := lookup(EnvBaseURL); ok {
			base = strings.TrimSpace(fromEnv)
		}
	}
	if base == "" {
		return ResolvedEndpoint{}, fmt.Errorf("%w: an endpoint URL is required", ErrInvalidEndpoint)
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return ResolvedEndpoint{}, fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ResolvedEndpoint{}, fmt.Errorf("%w: scheme %q is not http or https", ErrInvalidEndpoint, parsed.Scheme)
	}
	if parsed.Host == "" {
		return ResolvedEndpoint{}, fmt.Errorf("%w: the endpoint has no host", ErrInvalidEndpoint)
	}

	model := strings.TrimSpace(config.Model)
	if model == "" {
		if fromEnv, ok := lookup(EnvModel); ok {
			model = strings.TrimSpace(fromEnv)
		}
	}
	if model == "" {
		return ResolvedEndpoint{}, fmt.Errorf("%w: a model identifier is required", ErrInvalidEndpoint)
	}

	resolved := ResolvedEndpoint{BaseURL: parsed.String(), Model: model, CredentialSource: SourceNone}
	// An explicit key wins even when the environment also has one.
	if key := strings.TrimSpace(config.APIKey); key != "" {
		resolved.APIKey = key
		resolved.CredentialSource = SourceExplicit
	} else if fromEnv, ok := lookup(EnvAPIKey); ok && strings.TrimSpace(fromEnv) != "" {
		resolved.APIKey = strings.TrimSpace(fromEnv)
		resolved.CredentialSource = SourceEnvironment
	}
	if resolved.APIKey == "" && !config.AllowAnonymous && !isLoopback(parsed) {
		return ResolvedEndpoint{}, fmt.Errorf("%w: %s", ErrMissingCredential, parsed.Redacted())
	}
	return resolved, nil
}

// isLoopback reports whether a URL points at this machine.
//
// It is deliberately narrow: only literal loopback hosts count. Treating any
// host reachable over a private network as "local" would let an endpoint on a
// shared machine be reached without a credential.
func isLoopback(parsed *url.URL) bool {
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	if host == "::1" || host == "0:0:0:0:0:0:0:1" {
		return true
	}
	return strings.HasPrefix(host, "127.")
}

// Redact removes any credential from a URL for display.
//
// It exists because an endpoint URL can carry credentials in its userinfo, and a
// diagnostic that printed the URL verbatim would write the credential into a log.
func Redact(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "<unparseable endpoint>"
	}
	// Redacted replaces a password in the userinfo with a placeholder, which is
	// the field an endpoint URL can actually carry a secret in.
	return parsed.Redacted()
}

// Redacted returns a display form with the credential removed.
func (e ResolvedEndpoint) Redacted() string {
	parsed, err := url.Parse(e.BaseURL)
	if err != nil {
		return "<unparseable endpoint>"
	}
	// Redacted replaces a password in the userinfo with a placeholder, which is
	// the field an endpoint URL can actually carry a secret in.
	return parsed.Redacted()
}

// Describe reports the endpoint for diagnostics, with every secret removed.
//
// It reports the credential's *source* and a length signal rather than the value:
// an operator can tell "the environment supplied one" from "code supplied one"
// without the log ever holding the secret.
func (e ResolvedEndpoint) Describe() string {
	credential := "none"
	if e.APIKey != "" {
		credential = fmt.Sprintf("%s (%d chars)", e.CredentialSource, len(e.APIKey))
	}
	return fmt.Sprintf("endpoint=%s model=%s credential=%s", e.Redacted(), e.Model, credential)
}

// FromResolved builds an adapter configuration from a resolved endpoint.
func FromResolved(resolved ResolvedEndpoint, id, name string) Config {
	return Config{ID: id, Name: name, Model: resolved.Model, BaseURL: resolved.BaseURL, APIKey: resolved.APIKey}
}
