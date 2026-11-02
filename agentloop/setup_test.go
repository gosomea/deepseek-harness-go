package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

func TestNewFactoryValidatesItsConfiguration(t *testing.T) {
	if _, err := agentloop.NewFactory(agentloop.Config{}); !errors.Is(err, agentloop.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig without a provider, got %v", err)
	}
	if _, err := agentloop.NewFactory(agentloop.Config{Provider: testkit.NewFakeProvider()}); !errors.Is(err, agentloop.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig without a log, got %v", err)
	}
	factory := newFactory(t, agentloop.Config{Provider: testkit.NewFakeProvider()})
	if factory.Config().Provider == nil {
		t.Fatal("Config must return the configured provider")
	}
	if factory.Closed() {
		t.Fatal("a fresh factory is open")
	}
	if err := factory.Install(agent.NewRegistry()); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func TestRegistryCreateRunsThroughTheDefaultDriver(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"through the registry"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	registry := agent.NewRegistry()
	if err := factory.Install(registry); err != nil {
		t.Fatalf("install: %v", err)
	}
	owner, err := registry.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := owner.Handle().Send(context.Background(), agent.Input{Text: "hi"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(owner.Handle()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := registry.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if owner.Handle().State() != agent.StateDisposed {
		t.Fatalf("closing the registry must dispose its Agents, got %s", owner.Handle().State())
	}
	if !factory.Closed() {
		t.Fatal("closing the registry must close the factory")
	}
}
