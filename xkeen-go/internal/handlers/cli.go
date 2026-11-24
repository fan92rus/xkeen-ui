package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"

	"github.com/fan92rus/xkeen-ui/internal/config"
	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// CLI helpers — run the same handler logic as the web UI from the command
// line (for setup/debugging on the router without a browser session).

// NewCLIHandler builds an AWGHandler from the on-disk config.
// configPath is the path to config.json (the subscription store sits next to it).
func NewCLIHandler(cfg *config.Config, configPath string) (*AWGHandler, error) {
	storePath := filepath.Join(filepath.Dir(configPath), "subscriptions.json")
	store, err := subscription.NewStore(storePath)
	if err != nil {
		return nil, err
	}
	return NewAWGHandler(store, cfg.AWGConfigDir, cfg), nil
}

// cliJSONRequest builds a POST request with a JSON body via encoding/json
// (Go's %q is not JSON encoding — it can emit \xNN escapes).
func cliJSONRequest(path string, payload interface{}) (*http.Request, *httptest.ResponseRecorder, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	return httptest.NewRequest("POST", path, strings.NewReader(string(body))), httptest.NewRecorder(), nil
}

// CLICreateServer creates a server config via the normal HTTP handler path.
func CLICreateServer(h *AWGHandler, name, subnet string, port int, obfuscation string, params map[string]string) (string, error) {
	if params == nil {
		params = map[string]string{}
	}
	req, rec, err := cliJSONRequest("/api/awg/server/create", map[string]interface{}{
		"name": name, "subnet": subnet, "port": port, "obfuscation": obfuscation, "params": params,
	})
	if err != nil {
		return "", err
	}
	h.CreateServer(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		return "", fmt.Errorf("create failed (%d): %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String(), nil
}

// CLIUp starts an interface via the normal handler path.
func CLIUp(h *AWGHandler, name string) (string, error) {
	req, rec, err := cliJSONRequest("/api/awg/up", map[string]string{"name": name})
	if err != nil {
		return "", err
	}
	h.UpInterface(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		return "", fmt.Errorf("up failed (%d): %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String(), nil
}

// CLIDown stops an interface via the normal handler path.
func CLIDown(h *AWGHandler, name string) (string, error) {
	req, rec, err := cliJSONRequest("/api/awg/down", map[string]string{"name": name})
	if err != nil {
		return "", err
	}
	h.DownInterface(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		return "", fmt.Errorf("down failed (%d): %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String(), nil
}

// CLIDNSSync applies (or removes) native DNS zone forwarding and returns a
// status message. Used by `xkeen-ui dns-sync` and the startup hook.
func CLIDNSSync(cfg *config.Config, configPath string) (string, error) {
	return SyncDNSForward(cfg, configPath)
}

// CLIWatchdog restarts any dead OpkgTun server daemon. Safe to run from cron
// every minute: running daemons are left alone.
func CLIWatchdog(h *AWGHandler) (string, error) {
	configs, err := h.store.ScanAWGConfigs(h.awgDir)
	if err != nil {
		return "", err
	}
	restarted := make([]string, 0, len(configs))
	for _, c := range configs {
		if c.Role != "server" || !strings.HasPrefix(c.Name, "opkgtun") {
			continue
		}
		if pid, err := opkgDaemonPIDFn(c.Name); err == nil && pid > 0 {
			continue // running
		}
		if !isServerEnabled(c.Name) {
			continue // intentionally stopped by user
		}
		if _, err := CLIUp(h, c.Name); err != nil {
			log.Printf("[awg] watchdog: restart %s failed: %v", c.Name, err)
			continue
		}
		restarted = append(restarted, c.Name)
	}
	if len(restarted) == 0 {
		return "all AWG server daemons running", nil
	}
	return "restarted: " + strings.Join(restarted, ", "), nil
}
