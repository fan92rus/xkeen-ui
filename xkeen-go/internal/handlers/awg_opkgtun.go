package handlers

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// OpkgTun attach mode (KeeneticOS 5.x).
//
// On KeeneticOS 5+ a server interface can run through the native OpkgTun
// netdev (created via `ndmc -c "interface OpkgTun0 ... ip address ... up"` and
// saved in the system config). NDMS then owns the interface: its IP, MTU,
// connected route and the inter-zone firewall rules survive NDMS iptables
// rebuilds.
//
// In this mode xkeen-ui must NOT run awg-quick (it would create/delete the
// netdev and fight with NDMS). Instead it attaches the amneziawg-go daemon to
// the existing netdev and applies the config with `awg setconf`. Address, DNS
// and MTU lines are stripped from the runtime config because NDMS assigns
// them. The two rules NDMS cannot express natively (INPUT udp <listen-port>
// and the client NAT) are still applied by ApplyFullTunnelFirewall and
// restored by the NDM netfilter hook (RestoreAWGServerFirewalls) like any
// other server interface.

// Injection points for tests.
var (
	opkgLinkExistsFn = func(name string) bool {
		return exec.Command("ip", "link", "show", name).Run() == nil
	}
	// opkgNDMSHasIfaceFn checks whether Keenetic's NDMS config has the OpkgTun
	// interface (registered but netdev may not exist until daemon start).
	opkgNDMSHasIfaceFn = func(name string) bool {
		ndmsName := "OpkgTun" + strings.TrimPrefix(name, "opkgtun")
		out, err := exec.Command("ndmc", "-c", "show interface "+ndmsName).CombinedOutput() //nolint:gosec // ndmsName derived from validated config name
		return err == nil && strings.Contains(string(out), ndmsName)
	}
	opkgProcessRunningFn = func(name string) bool {
		return exec.Command("pgrep", "-f", opkgProcPattern(name)).Run() == nil //nolint:gosec // name validated by validateAWGName
	}
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
	opkgLookPathFn = exec.LookPath
	opkgSleepFn    = func(d time.Duration) { time.Sleep(d) }
	// opkgStartDaemonFn starts amneziawg-go detached from this process.
	opkgStartDaemonFn = func(bin, iface string) error {
		cmd := exec.Command(bin, iface)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return cmd.Start()
	}
)

// opkgProcPattern returns the pgrep/pkill pattern for an attach-mode
// daemon. End-anchored: without '$' a check for opkgtun0 would also match
// a daemon running as opkgtun01 (prefix collision), killing the wrong one.
func opkgProcPattern(name string) string {
	return "amneziawg-go " + name + "$"
}

// isOpkgTunAttachMode reports whether the interface runs in OpkgTun attach
// mode: the config name matches the Keenetic OpkgTun convention AND either
// the netdev exists or the interface is registered in NDMS's config.
// Keenetic 5.x does not hot-add OpkgTun netdevs — after CreateServer the
// interface is in the NDMS config but has no netdev; amneziawg-go creates it
// when started, so attach mode must apply in that "cold" state too.
func isOpkgTunAttachMode(name string) bool {
	if !strings.HasPrefix(name, "opkgtun") {
		return false
	}
	return opkgLinkExistsFn(name) || opkgNDMSHasIfaceFn(name)
}

// isOpkgTunOwned reports whether the name is in the OpkgTun namespace and
// either the netdev or an attach daemon still exists. Used by the DOWN paths
// (Down/Delete/restart): if the NDMS netdev was deleted in the Keenetic UI
// while the daemon is still running, awg-quick must still not be invoked —
// it would fail forever on an interface it does not own.
func isOpkgTunOwned(name string) bool {
	if !strings.HasPrefix(name, "opkgtun") {
		return false
	}
	return opkgLinkExistsFn(name) || opkgProcessRunningFn(name) || opkgNDMSHasIfaceFn(name)
}

// runtimeConfForOpkgTun copies the config to a runtime setconf file without
// the NDMS-owned keys (Address, DNS, MTU).
func runtimeConfForOpkgTun(name, confPath string) (string, error) {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return "", err
	}
	kept := make([]string, 0, 16)
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "address") || strings.HasPrefix(lower, "dns") || strings.HasPrefix(lower, "mtu") {
			continue
		}
		kept = append(kept, line)
	}
	// Unique per-run name (os.CreateTemp): two concurrent Ups on the same
	// interface must not delete each other's file between create and setconf.
	// CreateTemp uses O_EXCL (symlink-safe) and creates the file 0600. The
	// file holds the private key and is removed by the caller after setconf.
	f, err := os.CreateTemp(os.TempDir(), "xkeen-ui-"+name+"-*.set")
	if err != nil {
		return "", err
	}
	_, werr := f.WriteString(strings.Join(kept, "\n"))
	cerr := f.Close()
	if werr != nil {
		return "", werr
	}
	if cerr != nil {
		return "", cerr
	}
	return f.Name(), nil
}

// listenPortFromConf extracts ListenPort from the config (0 if unset).
func listenPortFromConf(confPath string) int {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "listenport") {
			var port int
			if _, err := fmt.Sscanf(trimmed, "ListenPort = %d", &port); err == nil {
				return port
			}
			// case-insensitive fallback
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				if n, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &port); err == nil && n == 1 {
					return port
				}
			}
		}
	}
	return 0
}

// startOpkgTunAttach brings a server interface up in OpkgTun attach mode:
// starts amneziawg-go on the existing netdev (if not already running), waits
// for its control socket, applies the config and enforces the listen port
// (setconf silently keeps a random port when the desired one was busy).
// opkgMu serializes the critical sections of attach start/stop (check+start
// daemon, pkill, setconf) so two concurrent Ups cannot start two daemons.
// The long waits (socket readiness, daemon death) deliberately run OUTSIDE
// the lock — otherwise one wedged daemon (socket never appears) would block
// Down for every interface.
var opkgMu sync.Mutex

func startOpkgTunAttach(name, confPath string) error {
	bin, err := opkgLookPathFn("amneziawg-go")
	if err != nil {
		if _, err2 := os.Stat("/opt/bin/amneziawg-go"); err2 != nil {
			return fmt.Errorf("amneziawg-go not found — install AWG tools via Settings first: %w", err)
		}
		bin = "/opt/bin/amneziawg-go"
	}

	opkgMu.Lock()
	if !opkgProcessRunningFn(name) {
		if err := opkgStartDaemonFn(bin, name); err != nil {
			opkgMu.Unlock()
			return fmt.Errorf("starting amneziawg-go: %w", err)
		}
	}
	opkgMu.Unlock()

	// Wait for the userspace control socket (awg show works) — outside the
	// lock so a wedged daemon cannot block Down/stop of other interfaces.
	ready := false
	for i := 0; i < 20; i++ {
		if _, err := opkgExecFn("awg", "show", name); err == nil {
			ready = true
			break
		}
		opkgSleepFn(500 * time.Millisecond)
	}
	if !ready {
		return errors.New("amneziawg-go control socket did not appear within 10s")
	}

	runtimeConf, err := runtimeConfForOpkgTun(name, confPath)
	if err != nil {
		return fmt.Errorf("preparing runtime config: %w", err)
	}
	// The file contains the private key: never leave it in the world-readable
	// temp dir, whatever happens below.
	defer os.Remove(runtimeConf) //nolint:errcheck // best-effort cleanup

	opkgMu.Lock()
	defer opkgMu.Unlock()
	if out, err := opkgExecFn("awg", "setconf", name, runtimeConf); err != nil {
		return fmt.Errorf("awg setconf failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}

	// setconf can silently keep a random listen port (e.g. when the desired
	// port was still held by a previous daemon): enforce it explicitly.
	// The line is terminated with \n: a bare substring would prefix-match
	// (443 would match "listening port: 4430").
	if port := listenPortFromConf(confPath); port != 0 {
		out, err := opkgExecFn("awg", "show", name)
		if err == nil && !strings.Contains(string(out), fmt.Sprintf("listening port: %d\n", port)) {
			if out2, err2 := opkgExecFn("awg", "set", name, "listen-port", fmt.Sprintf("%d", port)); err2 != nil {
				return fmt.Errorf("awg set listen-port failed: %v\n%s", err2, strings.TrimSpace(string(out2)))
			}
			// `awg set listen-port` clears the rest of the config: re-apply.
			if out3, err3 := opkgExecFn("awg", "setconf", name, runtimeConf); err3 != nil {
				return fmt.Errorf("awg setconf (after listen-port) failed: %v\n%s", err3, strings.TrimSpace(string(out3)))
			}
		}
	}

	log.Printf("[awg] OpkgTun attach: %s up (config %s)", name, confPath)
	return nil
}

// stopOpkgTunAttach brings the attach-mode interface down: kills the daemon
// and sets the netdev down. The netdev itself is never deleted — NDMS owns it.
func stopOpkgTunAttach(name string) error {
	opkgMu.Lock()
	_, _ = opkgExecFn("pkill", "-f", opkgProcPattern(name))
	opkgMu.Unlock()
	// Wait for the daemon to die outside the lock.
	for i := 0; i < 10 && opkgProcessRunningFn(name); i++ {
		opkgSleepFn(500 * time.Millisecond)
	}
	if opkgProcessRunningFn(name) {
		return fmt.Errorf("amneziawg-go %s did not stop within 5s", name)
	}
	if out, err := opkgExecFn("ip", "link", "set", name, "down"); err != nil {
		return fmt.Errorf("ip link set %s down: %v\n%s", name, err, strings.TrimSpace(string(out)))
	}
	log.Printf("[awg] OpkgTun attach: %s down", name)
	return nil
}
