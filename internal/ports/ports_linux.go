//go:build linux

package ports

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listenPortsForPID cross-references the target process's open file
// descriptors against the kernel's global TCP socket tables - the
// same approach `ss`/`netstat`/`lsof` use on Linux, just without
// shelling out: no /proc/<pid>/net/tcp per-process view exists (only
// a global one), so a socket found there only counts if one of this
// PID's own fds actually points at it.
func listenPortsForPID(pid int) ([]int, error) {
	inodes, err := ownedSocketInodes(pid)
	if err != nil {
		return nil, err
	}
	if len(inodes) == 0 {
		return nil, nil
	}

	var out []int
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		ports, err := listeningPortsIn(path, inodes)
		if err != nil {
			continue // best-effort: IPv6 disabled, unreadable, etc. - the other table can still contribute
		}
		out = append(out, ports...)
	}
	return out, nil
}

// ownedSocketInodes returns the inode number (as it appears in
// /proc/net/tcp*) of every socket fd open under /proc/<pid>/fd/.
func ownedSocketInodes(pid int) (map[string]bool, error) {
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// The common case: the process has already exited (or
			// hasn't started yet) - "nothing to report", not an error.
			return nil, nil
		}
		return nil, err
	}
	inodes := make(map[string]bool)
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // fd closed between ReadDir and Readlink - fine, skip it
		}
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inodes[target[len("socket:["):len(target)-1]] = true
		}
	}
	return inodes, nil
}

// listeningPortsIn parses one of /proc/net/tcp or /proc/net/tcp6 and
// returns the local port of every LISTEN-state row whose inode is in
// inodes. Field layout (whitespace-separated, after the header row):
// sl local_address rem_address st tx_queue:rx_queue tr:tm->when
// retrnsmt uid timeout inode ...
func listeningPortsIn(path string, inodes map[string]bool) ([]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	const (
		fieldLocalAddr = 1
		fieldState     = 3
		fieldInode     = 9
		stateListen    = "0A"
	)

	var out []int
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) <= fieldInode {
			continue
		}
		if fields[fieldState] != stateListen {
			continue
		}
		if !inodes[fields[fieldInode]] {
			continue
		}
		if port, ok := parseHexPort(fields[fieldLocalAddr]); ok {
			out = append(out, port)
		}
	}
	return out, sc.Err()
}

// parseHexPort extracts the port from a "<hex-addr>:<hex-port>"
// local_address field.
func parseHexPort(localAddr string) (int, bool) {
	i := strings.LastIndexByte(localAddr, ':')
	if i < 0 {
		return 0, false
	}
	port, err := strconv.ParseUint(localAddr[i+1:], 16, 16)
	if err != nil {
		return 0, false
	}
	return int(port), true
}
