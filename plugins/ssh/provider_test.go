package ssh_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/ssh"
)

func TestNewRequiresAbsoluteRoot(t *testing.T) {
	if _, err := ssh.New("relative"); err == nil {
		t.Fatal("relative root should fail")
	}
	if _, err := ssh.New("/srv/app"); err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestReadWriteInsideRoot(t *testing.T) {
	p, _ := ssh.New("/srv/app")
	if err := p.WriteFile("/srv/app/a.txt", "hello"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := p.ReadFile("/srv/app/a.txt")
	if err != nil || got != "hello" {
		t.Fatalf("Read = %q err=%v", got, err)
	}
}

func TestPathEscapes(t *testing.T) {
	p, _ := ssh.New("/srv/app")
	if err := p.WriteFile("/etc/passwd", "x"); !errors.Is(err, ssh.ErrPathEscapes) {
		t.Fatalf("escape err = %v", err)
	}
	if err := p.WriteFile("relative", "x"); !errors.Is(err, ssh.ErrPathEscapes) {
		t.Fatalf("relative err = %v", err)
	}
	if _, err := p.ReadFile("/srv/other/x"); !errors.Is(err, ssh.ErrPathEscapes) {
		t.Fatalf("read escape err = %v", err)
	}
}

func TestDisconnectFailsEverything(t *testing.T) {
	p, _ := ssh.New("/srv/app")
	_ = p.WriteFile("/srv/app/a.txt", "v")
	p.Disconnect()
	if p.Connected() {
		t.Fatal("should be disconnected")
	}
	if _, err := p.ReadFile("/srv/app/a.txt"); !errors.Is(err, ssh.ErrDisconnected) {
		t.Fatalf("read err = %v", err)
	}
	if err := p.WriteFile("/srv/app/b", "x"); !errors.Is(err, ssh.ErrDisconnected) {
		t.Fatalf("write err = %v", err)
	}
	if _, err := p.RunCommand("ls"); !errors.Is(err, ssh.ErrDisconnected) {
		t.Fatalf("cmd err = %v", err)
	}
	p.Reconnect()
	if !p.Connected() {
		t.Fatal("reconnect failed")
	}
}

func TestMissingFileAndCommands(t *testing.T) {
	p, _ := ssh.New("/srv/app")
	if _, err := p.ReadFile("/srv/app/none"); err == nil {
		t.Fatal("missing file should error")
	}
	if code, err := p.RunCommand("echo"); err != nil || code != 0 {
		t.Fatalf("run = %d err=%v", code, err)
	}
	if p.CommandCount() != 1 {
		t.Fatalf("count = %d", p.CommandCount())
	}
}
