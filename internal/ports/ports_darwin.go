//go:build darwin

package ports

import (
	"bufio"
	"bytes"
	"os/exec"
	"strconv"
	"strings"
)

// listenPortsForPID shells out to lsof, which ships with macOS by
// default - there's no /proc filesystem to inspect directly, and the
// alternative (calling libproc's proc_pidinfo/proc_pidfdinfo, what
// lsof itself uses internally) means reimplementing lsof rather than
// just using it.
//
// -a ANDs the following selectors, -iTCP restricts to TCP sockets,
// -sTCP:LISTEN to listening ones, -n/-P skip hostname/port-name
// resolution (faster, and gives numeric ports instead of service
// names like "http").
func listenPortsForPID(pid int) ([]int, error) {
	cmd := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN", "-n", "-P")
	out, err := cmd.Output()
	if err != nil {
		// lsof exits non-zero when a process has no matching (or no)
		// open files, which is the common "not listening on anything"
		// case, not a real error - only genuinely missing/unusable
		// lsof should be reported.
		if _, ok := err.(*exec.ExitError); ok {
			return nil, nil
		}
		return nil, err
	}
	return parseLsofOutput(out), nil
}

// parseLsofOutput extracts ports from lsof -n -P's NAME column, which
// for a listening TCP socket looks like "*:8080" or "127.0.0.1:8080"
// or "[::1]:8080", each followed by a separate "(LISTEN)" token.
func parseLsofOutput(out []byte) []int {
	var ports []int
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		name := fields[len(fields)-1]
		if strings.EqualFold(name, "(LISTEN)") {
			if len(fields) < 2 {
				continue
			}
			name = fields[len(fields)-2]
		}
		if port, ok := parseAddrPort(name); ok {
			ports = append(ports, port)
		}
	}
	return ports
}

// parseAddrPort extracts the port from an "addr:port" string, where
// addr may be "*", an IPv4 address, or a bracketed IPv6 address -
// the port always follows the last colon.
func parseAddrPort(addrPort string) (int, bool) {
	i := strings.LastIndexByte(addrPort, ':')
	if i < 0 {
		return 0, false
	}
	port, err := strconv.Atoi(addrPort[i+1:])
	if err != nil {
		return 0, false
	}
	return port, true
}
