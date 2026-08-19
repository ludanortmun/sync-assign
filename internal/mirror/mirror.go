package mirror

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
)

const defaultBranch = "main"

// Mirror is a prepared teacher repository. Close removes only ephemeral
// mirrors; persistent and explicitly configured paths are never removed.
type Mirror struct {
	path      string
	ephemeral bool

	closeOnce sync.Once
	closeErr  error
}

// Open prepares the mirror described by cfg using the git executable on PATH.
func Open(ctx context.Context, cfg config.StudentConfig) (*Mirror, error) {
	return OpenWithClient(ctx, cfg, gitcmd.New())
}

// OpenWithClient prepares the mirror described by cfg using client.
func OpenWithClient(ctx context.Context, cfg config.StudentConfig, client *gitcmd.Client) (*Mirror, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate mirror configuration: %w", err)
	}
	if client == nil {
		return nil, errors.New("prepare mirror: git client must not be nil")
	}

	ephemeral := cfg.MirrorIsEphemeral() != nil && *cfg.MirrorIsEphemeral()
	if ephemeral && cfg.TeacherPath != nil {
		return nil, errors.New("prepare mirror: teacher path and ephemeral mode cannot be used together")
	}

	branch := defaultBranch
	if cfg.Branch != nil {
		branch = *cfg.Branch
	}

	if ephemeral {
		return openEphemeral(ctx, client, cfg.TeacherRepository, branch)
	}

	if cfg.TeacherPath != nil {
		path, err := filepath.Abs(*cfg.TeacherPath)
		if err != nil {
			return nil, fmt.Errorf("resolve teacher path %q: %w", *cfg.TeacherPath, err)
		}
		return openAt(ctx, client, cfg.TeacherRepository, branch, path, true)
	}

	path, err := PersistentPath(cfg.TeacherRepository)
	if err != nil {
		return nil, err
	}
	return openAt(ctx, client, cfg.TeacherRepository, branch, path, false)
}

// PersistentPath returns the deterministic cache location for repository.
func PersistentPath(repository string) (string, error) {
	if strings.TrimSpace(repository) == "" {
		return "", errors.New("resolve persistent mirror: teacher repository must not be empty")
	}

	cacheRoot := os.Getenv("XDG_CACHE_HOME")
	if cacheRoot != "" {
		if !filepath.IsAbs(cacheRoot) {
			return "", fmt.Errorf("resolve persistent mirror: XDG_CACHE_HOME %q must be absolute", cacheRoot)
		}
	} else {
		var err error
		cacheRoot, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve persistent mirror cache directory: %w", err)
		}
	}

	sum := sha256.Sum256([]byte(repository))
	key := hex.EncodeToString(sum[:])
	return filepath.Join(cacheRoot, "sync-assign", key), nil
}

// Path returns the local teacher repository path.
func (m *Mirror) Path() string {
	if m == nil {
		return ""
	}
	return m.path
}

// Close releases the mirror. It is safe to call more than once.
func (m *Mirror) Close() error {
	if m == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		if m.ephemeral {
			if err := os.RemoveAll(m.path); err != nil {
				m.closeErr = fmt.Errorf("remove ephemeral mirror %q: %w", m.path, err)
			}
		}
	})
	return m.closeErr
}

func openEphemeral(ctx context.Context, client *gitcmd.Client, repository, branch string) (*Mirror, error) {
	path, err := os.MkdirTemp("", "sync-assign-mirror-*")
	if err != nil {
		return nil, fmt.Errorf("create ephemeral mirror: %w", err)
	}

	mirror := &Mirror{path: path, ephemeral: true}
	if err := client.Clone(ctx, repository, path, branch); err != nil {
		cleanupErr := mirror.Close()
		if cleanupErr != nil {
			return nil, errors.Join(fmt.Errorf("clone ephemeral mirror: %w", err), cleanupErr)
		}
		return nil, fmt.Errorf("clone ephemeral mirror: %w", err)
	}
	return mirror, nil
}

func openAt(
	ctx context.Context,
	client *gitcmd.Client,
	repository string,
	branch string,
	path string,
	userProvided bool,
) (*Mirror, error) {
	info, err := os.Stat(path)
	switch {
	case err == nil:
		if !info.IsDir() {
			return nil, fmt.Errorf("validate existing mirror %q: path is not a directory", path)
		}
		if err := validateWorktree(path); err != nil {
			return nil, err
		}
		if err := client.UpdateMirror(ctx, path, branch); err != nil {
			return nil, fmt.Errorf("update mirror %q on branch %q: %w", path, branch, err)
		}
	case errors.Is(err, os.ErrNotExist):
		if userProvided {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, fmt.Errorf("create parent directory for mirror %q: %w", path, err)
			}
			if err := client.Clone(ctx, repository, path, branch); err != nil {
				return nil, fmt.Errorf("clone mirror into configured path %q: %w", path, err)
			}
		} else if err := clonePersistent(ctx, client, repository, branch, path); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("inspect mirror %q: %w", path, err)
	}

	return &Mirror{path: path}, nil
}

func clonePersistent(ctx context.Context, client *gitcmd.Client, repository, branch, path string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create mirror cache directory %q: %w", parent, err)
	}

	staging, err := os.MkdirTemp(parent, "."+filepath.Base(path)+".clone-*")
	if err != nil {
		return fmt.Errorf("create clone staging directory for %q: %w", path, err)
	}
	defer os.RemoveAll(staging)

	if err := client.Clone(ctx, repository, staging, branch); err != nil {
		return fmt.Errorf("clone persistent mirror %q: %w", path, err)
	}
	if err := os.Rename(staging, path); err != nil {
		return fmt.Errorf("install persistent mirror %q: %w", path, err)
	}
	return nil
}

func validateWorktree(path string) error {
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("validate existing mirror %q: git metadata is missing", path)
		}
		return fmt.Errorf("validate existing mirror %q: inspect git metadata: %w", path, err)
	}
	return nil
}
