package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// buildTestDat constructs a minimal .dat protobuf (no leading type byte).
func buildTestDat(categories []string) []byte {
	var buf []byte
	for _, cat := range categories {
		var inner []byte
		// field 1: country_code (string)
		inner = protowire.AppendTag(inner, 1, protowire.BytesType)
		inner = protowire.AppendString(inner, cat)
		// outer: repeated field 1 (entry)
		buf = protowire.AppendTag(buf, 1, protowire.BytesType)
		buf = protowire.AppendBytes(buf, inner)
	}
	return buf
}

func TestParseDatCategories_GeoSite(t *testing.T) {
	data := buildTestDat([]string{"google", "youtube", "netflix"})
	result, err := parseDatCategories(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 categories, got %d: %v", len(result), result)
	}
	expected := []string{"google", "youtube", "netflix"}
	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("result[%d] = %q, want %q", i, result[i], exp)
		}
	}
}

func TestParseDatCategories_GeoIP(t *testing.T) {
	data := buildTestDat([]string{"ru", "cn", "us", "private"})
	result, err := parseDatCategories(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 categories, got %d: %v", len(result), result)
	}
}

func TestParseDatCategories_SingleCategory(t *testing.T) {
	data := buildTestDat([]string{"single"})
	result, _ := parseDatCategories(data)
	if len(result) != 1 || result[0] != "single" {
		t.Errorf("expected [single], got %v", result)
	}
}

func TestParseDatCategories_Empty(t *testing.T) {
	result, err := parseDatCategories(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty for nil input, got %v", result)
	}
}

func TestParseDatCategories_EmptyInput(t *testing.T) {
	result, _ := parseDatCategories([]byte{})
	if len(result) != 0 {
		t.Errorf("expected empty for empty input, got %v", result)
	}
}

func TestParseRealFixture_GeoSiteZkeen(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "geosite_zkeen.dat"))
	if err != nil {
		t.Skipf("test fixture not found: %v", err)
	}
	categories, err := parseDatCategories(data)
	if err != nil {
		t.Fatalf("parseDatCategories failed on real .dat file: %v", err)
	}
	if len(categories) == 0 {
		t.Fatal("expected at least one category from real geosite file")
	}
	// Verify known categories from zkeen .dat exist.
	known := map[string]bool{"BYPASS": false, "CN": false, "DOMAINS": false}
	for _, c := range categories {
		if _, ok := known[c]; ok {
			known[c] = true
		}
	}
	for name, found := range known {
		if !found {
			t.Errorf("expected category %q not found in real .dat file", name)
		}
	}
}

func TestScanDatFiles_NoDir(t *testing.T) {
	result, err := scanDatFiles("/nonexistent/path/12345")
	if err != nil {
		t.Fatalf("scanDatFiles should not error on missing dir: %v", err)
	}
	if len(result.GeoSite) != 0 || len(result.GeoIP) != 0 {
		t.Error("expected empty result for missing dir")
	}
}

func TestScanDatFiles_Success(t *testing.T) {
	dir := t.TempDir()

	geoSite := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(geoSite, buildTestDat([]string{"google", "youtube"}), 0o644); err != nil {
		t.Fatal(err)
	}

	geoIP := filepath.Join(dir, "geoip.dat")
	if err := os.WriteFile(geoIP, buildTestDat([]string{"ru", "us"}), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := scanDatFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.GeoSite) != 2 {
		t.Errorf("expected 2 geosite categories, got %d: %v", len(result.GeoSite), result.GeoSite)
	}
	for _, cat := range result.GeoSite {
		if cat.File != "geosite.dat" {
			t.Errorf("expected file geosite.dat, got %q", cat.File)
		}
	}

	if len(result.GeoIP) != 2 {
		t.Errorf("expected 2 geoip categories, got %d: %v", len(result.GeoIP), result.GeoIP)
	}
	for _, cat := range result.GeoIP {
		if cat.File != "geoip.dat" {
			t.Errorf("expected file geoip.dat, got %q", cat.File)
		}
	}
}

func TestScanDatFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	result, err := scanDatFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.GeoSite) != 0 || len(result.GeoIP) != 0 {
		t.Error("expected empty result for empty dir")
	}
}

func TestRoutingCategoriesHandler_Success(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), buildTestDat([]string{"google"}), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := NewRoutingCategoriesHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/api/routing/categories", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var geo GeoCategories
	if err := json.NewDecoder(w.Body).Decode(&geo); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(geo.GeoSite) != 1 || geo.GeoSite[0].Name != "google" {
		t.Errorf("expected [[google]], got %v", geo)
	}
}

func TestRoutingCategoriesHandler_Empty(t *testing.T) {
	handler := NewRoutingCategoriesHandler(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/routing/categories", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var geo GeoCategories
	json.NewDecoder(w.Body).Decode(&geo)
	if len(geo.GeoSite) != 0 || len(geo.GeoIP) != 0 {
		t.Errorf("expected empty, got geosite=%d geoip=%d", len(geo.GeoSite), len(geo.GeoIP))
	}
}

func TestRoutingCategoriesHandler_MethodNotAllowed(t *testing.T) {
	handler := NewRoutingCategoriesHandler(t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/routing/categories", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestScanDatFiles_SkipsTmpFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "geosite.dat"), buildTestDat([]string{"good"}), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "geosite.dat.tmp"), buildTestDat([]string{"bad"}), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "geosite.tmp.dat"), buildTestDat([]string{"bad2"}), 0o644)

	result, err := scanDatFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.GeoSite) != 1 {
		t.Errorf("expected 1 geosite entry, got %d", len(result.GeoSite))
	}
}
