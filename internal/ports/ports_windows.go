//go:build windows

package ports

import (
	"bufio"
	"bytes"
	"os/exec"
	"strconv"
	"strings"
)

// listenPortsForPID shells out to netstat, which ships with Windows -
// there's no /proc filesystem, and the alternative (calling
// GetExtendedTcpTable from iphlpapi.dll for structured owning-PID/port
// data instead of parsing text) is a real option if this ever needs
// to be faster or more robust than a subprocess per poll, but isn't
// necessary to get this working.
func listenPortsForPID(pid int) ([]int, error) {
	cmd := exec.Command("netstat", "-ano", "-p", "TCP")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseNetstatOutput(out, pid), nil
}

// parseNetstatOutput extracts listening ports owned by pid from
// `netstat -ano` output. Each TCP row looks like:
//
//	TCP    0.0.0.0:8080           0.0.0.0:0              LISTENING       1234
//	TCP    [::]:8080              [::]:0                 LISTENING       1234
//
// i.e. Proto, LocalAddress, ForeignAddress, State, PID (whitespace-
// separated; UDP rows have no State column and are skipped since
// they don't match Proto=="TCP").
func parseNetstatOutput(out []byte, pid int) []int {
	pidStr := strconv.Itoa(pid)
	var ports []int
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 5 {
			continue
		}
		proto, localAddr, _, state, rowPID := fields[0], fields[1], fields[2], fields[3], fields[4]
		if !strings.EqualFold(proto, "TCP") || !strings.EqualFold(state, "LISTENING") || rowPID != pidStr {
			continue
		}
		if port, ok := parseAddrPort(localAddr); ok {
			ports = append(ports, port)
		}
	}
	return ports
}

// parseAddrPort extracts the port from an "addr:port" string, where
// addr may be an IPv4 address or a bracketed IPv6 address - the port
// always follows the last colon.
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
