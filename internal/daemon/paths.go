// Package daemon lets a `godev run ... --detach` instance keep running
// in the background after the launching command exits, and lets later
// `godev attach`/`godev kill` invocations find and talk to it. It is
// deliberately narrow: a Unix socket per project (no HTTP, no general
// RPC surface for hypothetical future clients) carrying exactly what
// internal/tui.Source needs, plus one shutdown message. There is no
// "daemon mode" beyond that - every other godev command still runs
// standalone and in-process, as before.
package daemon

import (
	"os"
	"path/filepath"

	"github.com/abtinokhovat/godev/internal/projectid"
)

// Paths are the per-project files a detached instance uses, all under
// the same cache directory internal/builder already namespaces by
// project (~/.cache/godev/<project-id>/), so they naturally share the
// same cleanup/collision story as the build cache.
type Paths struct {
	Dir    string
	Socket string
	PID    string
	Log    string // captures stray stdout/stderr (e.g. an early panic) once detached
}

// ResolvePaths computes Paths for projectRoot without creating
// anything on disk.
func ResolvePaths(projectRoot string) (Paths, error) {
	userCache, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, err
	}
	dir := filepath.Join(userCache, "godev", projectid.Hash(projectRoot))
	return Paths{
		Dir:    dir,
		Socket: filepath.Join(dir, "godev.sock"),
		PID:    filepath.Join(dir, "godev.pid"),
		Log:    filepath.Join(dir, "godev.detached.log"),
	}, nil
}
