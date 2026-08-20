// Package builder wraps `go build` to produce cached, atomically-swapped
// binaries for each service, in normal or debug (unoptimized) mode.
package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/abtinokhovat/godev/internal/domain"
)

// Mode selects build flags, per section 15.
type Mode int

const (
	ModeNormal Mode = iota
	ModeDebug
)

// Result is the outcome of a build attempt.
type Result struct {
	Success    bool
	BinaryPath string
	Output     string // combined stdout+stderr, useful on failure
}

// Builder builds services into a per-project cache directory under
// ~/.cache/godev/<project-id>/<service>/, per section 13.
type Builder struct {
	ProjectRoot string
	CacheDir    string
}

// New creates a Builder rooted at ~/.cache/godev/<project-id>.
func New(projectRoot string) (*Builder, error) {
	userCache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(projectRoot))
	projectID := hex.EncodeToString(sum[:])[:12]
	cacheDir := filepath.Join(userCache, "godev", projectID)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	return &Builder{ProjectRoot: projectRoot, CacheDir: cacheDir}, nil
}

// Build compiles svc and, on success, atomically installs the result as
// the service's "current" binary. On failure the previous "current"
// binary (if any) is left untouched, per section 14.
func (b *Builder) Build(svc domain.Service, mode Mode) (Result, error) {
	serviceDir := filepath.Join(b.CacheDir, svc.Name)
	if err := os.MkdirAll(serviceDir, 0o755); err != nil {
		return Result{}, err
	}

	suffix := "normal"
	if mode == ModeDebug {
		suffix = "debug"
	}
	tmpBinary := filepath.Join(serviceDir, fmt.Sprintf(".build-%s-%d", suffix, os.Getpid()))
	finalBinary := filepath.Join(serviceDir, "current-"+suffix)

	args := []string{"build"}
	if mode == ModeDebug {
		args = append(args, "-gcflags", "all=-N -l")
	}
	args = append(args, "-o", tmpBinary, svc.Package)

	cmd := exec.Command("go", args...)
	cmd.Dir = b.ProjectRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpBinary)
		return Result{Success: false, Output: string(out)}, nil
	}

	if err := os.Rename(tmpBinary, finalBinary); err != nil {
		return Result{}, fmt.Errorf("installing built binary: %w", err)
	}
	if err := os.Chmod(finalBinary, 0o755); err != nil {
		return Result{}, err
	}

	return Result{Success: true, BinaryPath: finalBinary, Output: string(out)}, nil
}
