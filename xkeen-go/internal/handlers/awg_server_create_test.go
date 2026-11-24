package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateServer_Valid(t *testing.T) {
	_, r, awgDir := newTestAWGHandler(t)
	orig := awgGenKeyFn
	t.Cleanup(func() { awgGenKeyFn = orig })
	awgGenKeyFn = func() (string, string, error) { return "PRIVKEY123", "PUBKEY456", nil }

	body := `{"name":"opkgtun1","subnet":"10.8.1.0/24","port":4443,"obfuscation":"full"}`
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/server/create", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	// config file written with key, address, port, params
	data, err := os.ReadFile(filepath.Join(awgDir, "opkgtun1.conf"))
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	for _, want := range []string{"PrivateKey = PRIVKEY123", "Address = 10.8.1.1/24", "ListenPort = 4443", "Jc = 8"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("config missing %q:\n%s", want, data)
		}
	}
	// response contains ndms commands
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp["ndms_commands"].(string), "interface OpkgTun1") {
		t.Fatalf("ndms_commands missing OpkgTun1:\n%v", resp["ndms_commands"])
	}
}

func TestCreateServer_InvalidName(t *testing.T) {
	_, r, _ := newTestAWGHandler(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/server/create", strings.NewReader(`{"name":"server","subnet":"10.8.1.0/24","port":4443}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateServer_DuplicateName(t *testing.T) {
	h, r, awgDir := newTestAWGHandler(t)
	os.WriteFile(filepath.Join(awgDir, "opkgtun1.conf"), []byte("[Interface]\nListenPort=4443\n"), 0o600)
	h.store.ScanAWGConfigs(awgDir) //nolint:errcheck
	orig := awgGenKeyFn
	t.Cleanup(func() { awgGenKeyFn = orig })
	awgGenKeyFn = func() (string, string, error) { return "k", "p", nil }

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/server/create", strings.NewReader(`{"name":"opkgtun1","subnet":"10.8.2.0/24","port":4444}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateServer_PortConflict(t *testing.T) {
	h, r, awgDir := newTestAWGHandler(t)
	os.WriteFile(filepath.Join(awgDir, "opkgtun0.conf"), []byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n"), 0o600)
	h.store.ScanAWGConfigs(awgDir) //nolint:errcheck
	orig := awgGenKeyFn
	t.Cleanup(func() { awgGenKeyFn = orig })
	awgGenKeyFn = func() (string, string, error) { return "k", "p", nil }

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/server/create", strings.NewReader(`{"name":"opkgtun1","subnet":"10.8.1.0/24","port":443}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateServer_SubnetConflict(t *testing.T) {
	h, r, awgDir := newTestAWGHandler(t)
	os.WriteFile(filepath.Join(awgDir, "opkgtun0.conf"), []byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n"), 0o600)
	h.store.ScanAWGConfigs(awgDir) //nolint:errcheck
	orig := awgGenKeyFn
	t.Cleanup(func() { awgGenKeyFn = orig })
	awgGenKeyFn = func() (string, string, error) { return "k", "p", nil }

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/server/create", strings.NewReader(`{"name":"opkgtun1","subnet":"10.8.0.0/24","port":4443}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateServer_InvalidSubnet(t *testing.T) {
	_, r, _ := newTestAWGHandler(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/server/create", strings.NewReader(`{"name":"opkgtun1","subnet":"10.8.1.5/24","port":4443}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateServer_InvalidPort(t *testing.T) {
	_, r, _ := newTestAWGHandler(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/server/create", strings.NewReader(`{"name":"opkgtun1","subnet":"10.8.1.0/24","port":0}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateServer_UnknownPreset(t *testing.T) {
	_, r, _ := newTestAWGHandler(t)
	orig := awgGenKeyFn
	t.Cleanup(func() { awgGenKeyFn = orig })
	awgGenKeyFn = func() (string, string, error) { return "k", "p", nil }
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/server/create", strings.NewReader(`{"name":"opkgtun1","subnet":"10.8.1.0/24","port":4443,"obfuscation":"nope"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateServer_CustomParams(t *testing.T) {
	_, r, awgDir := newTestAWGHandler(t)
	orig := awgGenKeyFn
	t.Cleanup(func() { awgGenKeyFn = orig })
	awgGenKeyFn = func() (string, string, error) { return "PRIV", "PUB", nil }

	body := `{"name":"opkgtun2","subnet":"10.8.2.0/24","port":4444,"obfuscation":"custom","params":{"Jc":"3","H1":"50"}}`
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/server/create", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(awgDir, "opkgtun2.conf"))
	if !strings.Contains(string(data), "Jc = 3") || !strings.Contains(string(data), "H1 = 50") {
		t.Fatalf("custom params not written:\n%s", data)
	}
}
