package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/client"
)

func TestNew_AdditionalPaths(t *testing.T) {
	orig := newDockerEngineClient
	t.Cleanup(func() { newDockerEngineClient = orig })

	newDockerEngineClient = func() (*client.Client, error) {
		return &client.Client{}, nil
	}

	t.Run("returns wrapped config load error", func(t *testing.T) {
		_, err := New(Config{
			ConfigPath: filepath.Join(t.TempDir(), "missing", "aegion.yaml"),
		})
		if err == nil || !strings.Contains(err.Error(), "loading config:") {
			t.Fatalf("expected wrapped load error, got %v", err)
		}
	})

	t.Run("uses internal secret when token secret is not provided", func(t *testing.T) {
		dir := t.TempDir()
		configPath := writeMinimalMainConfig(t, dir)

		o, err := New(Config{
			ConfigPath: configPath,
		})
		if err != nil {
			t.Fatalf("New returned error: %v", err)
		}
		if o.tokenGenerator == nil {
			t.Fatalf("expected token generator from internal secret")
		}
		if err := o.Stop(context.Background()); err != nil {
			t.Fatalf("Stop returned error: %v", err)
		}
	})
}
