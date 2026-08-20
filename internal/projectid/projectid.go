// Package projectid derives a short, stable identifier for a project
// root, used to namespace per-project state (the build cache, the
// detached-instance PID file and control socket) under a shared
// directory without collisions between different projects.
package projectid

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns a 12-character hex identifier for projectRoot. It is a
// pure function of the path - same root, same ID, every time.
func Hash(projectRoot string) string {
	sum := sha256.Sum256([]byte(projectRoot))
	return hex.EncodeToString(sum[:])[:12]
}
