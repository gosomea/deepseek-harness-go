package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Errors this package returns.
var (
	// ErrTooLarge reports a response body beyond the configured limit.
	ErrTooLarge = errors.New("web: response exceeds size limit")
	// ErrEmptyURL reports a fetch with no URL.
	ErrEmptyURL = errors.New("web: url is required")
)

// FetchResult is a bounded, structured fetch result.
//
// It carries the final URL (after redirects), a status code and a truncated body,
// so a caller can tell what it actually got. The body is data: nothing here
// interprets it as instructions.
type FetchResult struct {
	// URL is the final URL fetched, after redirects.
	URL string
	// Status is the HTTP status code.
	Status int
	// Body is the (possibly truncated) response body.
	Body string
	// Truncated reports whether the body was cut at the size limit.
	Truncated bool
}

// Fetcher retrieves a URL.
type Fetcher interface {
	// Fetch retrieves a URL under a size limit and a context.
	Fetch(ctx context.Context, url string, limit int) (FetchResult, error)
}

// Backend is the raw HTTP operation a provider wraps, so a test can supply a
// function instead of a socket.
//
// It returns the final URL, status and body, or an error. Bounding and truncation
// happen in the provider, not the backend, so the size rule is proven once.
type Backend func(ctx context.Context, url string) (finalURL string, status int, body io.Reader, err error)

// Provider is a Fetcher built over a Backend.
type Provider struct {
	backend Backend
}

// New returns a provider over a backend.
func New(backend Backend) (*Provider, error) {
	if backend == nil {
		return nil, fmt.Errorf("web: a backend is required")
	}
	return &Provider{backend: backend}, nil
}

// Fetch retrieves a URL, truncating the body at limit.
//
// A limit of zero or less means a default cap, so a caller cannot accidentally
// request an unbounded read. A body beyond the limit is truncated and flagged
// rather than returned whole, so memory stays bounded.
func (p *Provider) Fetch(ctx context.Context, url string, limit int) (FetchResult, error) {
	if strings.TrimSpace(url) == "" {
		return FetchResult{}, fmt.Errorf("%w", ErrEmptyURL)
	}
	if limit <= 0 {
		limit = 1 << 20 // 1 MiB default cap
	}
	finalURL, status, body, err := p.backend(ctx, url)
	if err != nil {
		return FetchResult{}, err
	}
	if body == nil {
		return FetchResult{URL: finalURL, Status: status}, nil
	}
	defer func() { _ = closeIfCloser(body) }()
	// Read at most limit+1 bytes: the extra byte detects truncation without
	// reading the whole (possibly huge) body into memory.
	buf, err := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	if err != nil {
		return FetchResult{}, err
	}
	truncated := len(buf) > limit
	if truncated {
		buf = buf[:limit]
	}
	return FetchResult{URL: finalURL, Status: status, Body: string(buf), Truncated: truncated}, nil
}

// SearchResult is one search hit.
type SearchResult struct {
	// Title is the hit title.
	Title string
	// URL is the hit URL.
	URL string
	// Snippet is a short excerpt.
	Snippet string
}

// Searcher finds pages matching a query.
type Searcher interface {
	// Search returns up to limit results for a query.
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// closeIfCloser closes a reader that is also a closer.
func closeIfCloser(r io.Reader) error {
	if c, ok := r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
