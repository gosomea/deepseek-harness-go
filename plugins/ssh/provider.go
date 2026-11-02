package ssh

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Errors this provider returns.
var (
	// ErrDisconnected reports use of a provider whose connection is down.
	ErrDisconnected = errors.New("ssh: connection is down")
	// ErrPathEscapes reports a path outside the remote root.
	ErrPathEscapes = errors.New("ssh: path escapes remote root")
)

// Provider is a remote execution seam over an in-memory model of a host.
//
// It models the two things that differ from a local world — a remote root that
// bounds every path, and a connection that can go down — so the seam's rules are
// checkable without a real host.
type Provider struct {
	mu          sync.Mutex
	root        string
	connected   bool
	files       map[string]string
	commandHits int
}

// New returns a provider rooted at a remote path, starting connected.
func New(root string) (*Provider, error) {
	if !strings.HasPrefix(root, "/") {
		return nil, fmt.Errorf("ssh: root must be absolute")
	}
	return &Provider{root: root, connected: true, files: map[string]string{}}, nil
}

// Disconnect takes the connection down; every operation then fails.
func (p *Provider) Disconnect() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = false
}

// Reconnect brings the connection back.
func (p *Provider) Reconnect() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = true
}

// Connected reports the connection state.
func (p *Provider) Connected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connected
}

// resolve maps a path into the remote root, refusing escapes.
func (p *Provider) resolve(path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("%w: %q is not absolute", ErrPathEscapes, path)
	}
	if !strings.HasPrefix(path, p.root+"/") && path != p.root {
		return "", fmt.Errorf("%w: %q is outside %q", ErrPathEscapes, path, p.root)
	}
	return path, nil
}

// ReadFile reads a file inside the remote root.
func (p *Provider) ReadFile(path string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.connected {
		return "", fmt.Errorf("%w", ErrDisconnected)
	}
	resolved, err := p.resolve(path)
	if err != nil {
		return "", err
	}
	content, ok := p.files[resolved]
	if !ok {
		return "", fmt.Errorf("ssh: no such file %q", resolved)
	}
	return content, nil
}

// WriteFile writes a file inside the remote root.
func (p *Provider) WriteFile(path, content string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.connected {
		return fmt.Errorf("%w", ErrDisconnected)
	}
	resolved, err := p.resolve(path)
	if err != nil {
		return err
	}
	p.files[resolved] = content
	return nil
}

// RunCommand records a command run and returns its exit code.
func (p *Provider) RunCommand(program string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.connected {
		return 0, fmt.Errorf("%w", ErrDisconnected)
	}
	p.commandHits++
	return 0, nil
}

// CommandCount reports how many commands ran, for tests to observe no-hidden-runs.
func (p *Provider) CommandCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.commandHits
}
