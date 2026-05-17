package skill

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"path/filepath"
	"strings"

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
		b, err := backendForDir(ctx, expandHome(dir))
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
// Returns nil, nil if dir does not exist or is not a directory.
func backendForDir(ctx context.Context, dir string) (skill.Backend, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, nil
	}
	lb, err := localbackend.NewBackend(ctx, &localbackend.Config{})
	if err != nil {
		return nil, err
	}
	backend, err := skill.NewBackendFromFilesystem(ctx, &skill.BackendFromFilesystemConfig{
		Backend: lb,
		BaseDir: dir,
	})
	if err != nil {
		log.Warn().Str("dir", dir).Err(err).Msg("skill: skipping directory")
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
		for i := range items {
			if _, dup := seen[items[i].Name]; dup {
				log.Warn().Str("name", items[i].Name).Msg("skill: duplicate skill name, skipping")
				continue
			}
			seen[items[i].Name] = struct{}{}
			all = append(all, items[i])
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
		for i := range items {
			if items[i].Name == name {
				return b.Get(ctx, name)
			}
		}
	}
	return skill.Skill{}, fmt.Errorf("skill %q not found", name)
}

// expandHome replaces a leading "~" with the current user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
