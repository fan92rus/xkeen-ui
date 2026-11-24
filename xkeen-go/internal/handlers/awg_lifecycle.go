package handlers

import (
	"errors"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// Interface lifecycle engine: the single place that knows how an AWG
// interface is brought up, torn down and verified — for quick-mode interfaces
// (awg-quick owns the netdev) and OpkgTun attach-mode interfaces (NDMS owns
// the netdev, amneziawg-go is attached to it). The HTTP handlers and the
// restart/delete flows all route through these functions so the orchestration
// can never drift apart again (it used to exist as three divergent copies).

// bringUp brings the interface up. Returns attach=true when OpkgTun attach
// mode was used. exec.ErrNotFound is mapped to an actionable message.
func (h *AWGHandler) bringUp(name, confPath string) (attach bool, err error) {
	// OpkgTun attach mode (KeeneticOS 5.x): the netdev is owned by NDMS;
	// attach amneziawg-go to it instead of running awg-quick.
	if isOpkgTunAttachMode(name) {
		if err := startOpkgTunAttach(name, confPath); err != nil {
			return true, fmt.Errorf("OpkgTun attach failed for %q: %w", name, err)
		}
		return true, nil
	}
	output, err := awgQuickFn("up", confPath)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, errors.New("awg-quick binary not found — install AWG tools via Settings → AWG Install first")
		}
		return false, fmt.Errorf("awg-quick up failed: %w\n%s", err, string(output))
	}
	log.Printf("[awg] awg-quick up %s: %s", name, string(output))
	return false, nil
}

// teardown takes the interface down and removes its routing/firewall state.
// role decides which pre-down hooks run (server: full-tunnel firewall;
// client: fwmark routing). tolerant=true (restart/delete) logs instead of
// failing — the interface may already be down.
func (h *AWGHandler) teardown(name, confPath string, role subscription.AWGRole, mark int, tolerant bool) error {
	// Pre-down hooks: remove routing/firewall BEFORE the interface goes down.
	h.preDown(name, role, mark)

	// Down. OpkgTun-owned netdevs are never deleted — detach the daemon.
	// Detection covers a still-running daemon even when the netdev is gone
	// (Keenetic-UI delete race).
	if isOpkgTunOwned(name) {
		if err := stopOpkgTunAttach(name); err != nil {
			if !tolerant {
				return fmt.Errorf("OpkgTun detach failed for %q: %w", name, err)
			}
			log.Printf("[awg] OpkgTun detach failed for %s: %v (continuing)", name, err)
		}
	} else if output, err := awgQuickFn("down", confPath); err != nil {
		if !tolerant {
			if errors.Is(err, exec.ErrNotFound) {
				return errors.New("awg-quick binary not found — install AWG tools via Settings → AWG Install first")
			}
			return fmt.Errorf("awg-quick down failed: %w\n%s", err, string(output))
		}
		log.Printf("[awg] awg-quick down failed for %s: %v (continuing)", name, err)
	}
	return nil
}

// postUp re-applies the role-based post-up state (server: full-tunnel
// firewall; client: fwmark routing).
func (h *AWGHandler) postUp(name string, role subscription.AWGRole, mark int) {
	if role == subscription.AWGRoleServer {
		ApplyFullTunnelFirewall(h.getServerFirewallParams(name))
	} else {
		h.applyClientFwmarkRouting(name, mark)
	}
}

// restartInterface does a full down + up cycle (required for AWG param
// changes, since syncconf cannot change transport-level obfuscation params).
// The post-up hooks always run, even when the up step fails: the pre-down
// already removed the firewall/routing, and leaving a production server
// without its rules would be worse than the restart error itself.
func (h *AWGHandler) restartInterface(name string, role subscription.AWGRole, mark int) error {
	confPath := confPathFor(h.awgDir, name)

	// Tolerant teardown: the interface may already be down.
	_ = h.teardown(name, confPath, role, mark, true)

	time.Sleep(500 * time.Millisecond)

	_, upErr := h.bringUp(name, confPath)
	// Post-up state is re-applied unconditionally (see comment above).
	h.postUp(name, role, mark)
	return upErr
}

// preDown removes the role-based routing/firewall state (used by DownInterface,
// which keeps its own richer down/verify handling for quick-mode interfaces).
func (h *AWGHandler) preDown(name string, role subscription.AWGRole, mark int) {
	if role == subscription.AWGRoleServer {
		RemoveFullTunnelFirewall(h.getServerFirewallParams(name))
	} else {
		h.removeClientFwmarkRouting(name, mark)
	}
}

// confPathFor returns the config file path for an AWG interface name.
func confPathFor(awgDir, name string) string {
	return filepath.Join(awgDir, name+".conf")
}
