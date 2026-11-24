package handlers

import (
	"fmt"
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

// CLICreateServer creates a server config via the normal HTTP handler path.
func CLICreateServer(h *AWGHandler, name, subnet string, port int, obfuscation string, params map[string]string) (string, error) {
	body := fmt.Sprintf(`{"name":%q,"subnet":%q,"port":%d,"obfuscation":%q,"params":{}}`, name, subnet, port, obfuscation)
	// params are intentionally empty for the CLI helper (presets only); the
	// web UI handles the custom-params case.
	req := httptest.NewRequest("POST", "/api/awg/server/create", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateServer(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		return "", fmt.Errorf("create failed (%d): %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String(), nil
}

// CLIUp starts an interface via the normal handler path.
func CLIUp(h *AWGHandler, name string) (string, error) {
	body := fmt.Sprintf(`{"name":%q}`, name)
	req := httptest.NewRequest("POST", "/api/awg/up", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpInterface(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		return "", fmt.Errorf("up failed (%d): %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String(), nil
}

// CLIDown stops an interface via the normal handler path.
func CLIDown(h *AWGHandler, name string) (string, error) {
	body := fmt.Sprintf(`{"name":%q}`, name)
	req := httptest.NewRequest("POST", "/api/awg/down", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.DownInterface(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		return "", fmt.Errorf("down failed (%d): %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String(), nil
}
