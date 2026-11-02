package web_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/web"
)

func TestFetchTruncates(t *testing.T) {
	body := strings.Repeat("a", 100)
	p, err := web.New(func(_ context.Context, u string) (string, int, io.Reader, error) {
		return u, 200, bytes.NewReader([]byte(body)), nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := p.Fetch(context.Background(), "https://example.test/x", 10)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !result.Truncated || len(result.Body) != 10 {
		t.Fatalf("result = %+v", result)
	}
	if result.Status != 200 || result.URL != "https://example.test/x" {
		t.Fatalf("result = %+v", result)
	}
}

func TestFetchFitsNoTruncation(t *testing.T) {
	p, _ := web.New(func(_ context.Context, u string) (string, int, io.Reader, error) {
		return u, 200, bytes.NewReader([]byte("short")), nil
	})
	result, err := p.Fetch(context.Background(), "u", 100)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.Truncated || result.Body != "short" {
		t.Fatalf("result = %+v", result)
	}
}

func TestFetchDefaultLimitAndNilBody(t *testing.T) {
	p, _ := web.New(func(_ context.Context, u string) (string, int, io.Reader, error) {
		return u, 204, nil, nil
	})
	result, err := p.Fetch(context.Background(), "u", 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.Body != "" || result.Truncated {
		t.Fatalf("nil body result = %+v", result)
	}
}

func TestFetchErrors(t *testing.T) {
	if _, err := web.New(nil); err == nil {
		t.Fatal("nil backend should fail")
	}
	p, _ := web.New(func(_ context.Context, u string) (string, int, io.Reader, error) {
		return u, 200, nil, nil
	})
	if _, err := p.Fetch(context.Background(), "  ", 10); !errors.Is(err, web.ErrEmptyURL) {
		t.Fatalf("empty url err = %v", err)
	}
	boom, _ := web.New(func(_ context.Context, u string) (string, int, io.Reader, error) {
		return "", 0, nil, errors.New("network down")
	})
	if _, err := boom.Fetch(context.Background(), "u", 10); err == nil {
		t.Fatal("expected backend error")
	}
}
