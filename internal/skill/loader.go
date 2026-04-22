package skill

import (
	"context"
	"fmt"
	"log/slog"

	localbackend "github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
)

// NewMiddleware builds a skill.Middleware from all directories in skillsDirs.
// Directories that do not exist are silently skipped. Returns nil, nil when
// skillsDirs is empty or no SKILL.md files are found in any directory.
func NewMiddleware(ctx context.Context, skillsDirs []string) (adk.ChatModelAgentMiddleware, error) {
	var backends []skill.Backend
	for _, dir := range skillsDirs {
		b, err := backendForDir(ctx, dir)
		if err != nil {
			return nil, err
		}
		if b != nil {
			backends = append(backends, b)
		}
	}
	if len(backends) == 0 {
		return nil, nil
	}

	backend := skill.Backend(&multiBackend{backends: backends})
	if len(backends) == 1 {
		backend = backends[0]
	}

	return skill.NewMiddleware(ctx, &skill.Config{Backend: backend})
}

// backendForDir creates a filesystem skill.Backend for a single directory.
// Returns nil, nil if dir does not exist.
func backendForDir(ctx context.Context, dir string) (skill.Backend, error) {
	lb, err := localbackend.NewBackend(ctx, &localbackend.Config{})
	if err != nil {
		return nil, err
	}
	backend, err := skill.NewBackendFromFilesystem(ctx, &skill.BackendFromFilesystemConfig{
		Backend: lb,
		BaseDir: dir,
	})
	if err != nil {
		// Non-existent base dir produces an error; log and skip.
		slog.Warn("skill: skipping directory", "dir", dir, "err", err)
		return nil, nil
	}
	return backend, nil
}

// multiBackend merges multiple skill.Backends into one, deduplicating by name.
type multiBackend struct {
	backends []skill.Backend
}

// List returns the union of all skills across backends, deduplicating by name.
func (m *multiBackend) List(ctx context.Context) ([]skill.FrontMatter, error) {
	seen := map[string]struct{}{}
	var all []skill.FrontMatter
	for _, b := range m.backends {
		items, err := b.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, fm := range items {
			if _, dup := seen[fm.Name]; dup {
				slog.Warn("skill: duplicate skill name, skipping", "name", fm.Name)
				continue
			}
			seen[fm.Name] = struct{}{}
			all = append(all, fm)
		}
	}
	return all, nil
}

// Get retrieves a skill by name from the first backend that contains it.
func (m *multiBackend) Get(ctx context.Context, name string) (skill.Skill, error) {
	for _, b := range m.backends {
		items, err := b.List(ctx)
		if err != nil {
			return skill.Skill{}, err
		}
		for _, fm := range items {
			if fm.Name == name {
				return b.Get(ctx, name)
			}
		}
	}
	return skill.Skill{}, fmt.Errorf("skill %q not found", name)
}
