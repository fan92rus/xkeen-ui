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

// buildTestDat constructs a minimal .dat file in protobuf wire format.
// fileType: 0x01 for geosite, 0x02 for geoip.
func buildTestDat(fileType byte, categories []string) []byte {
	var buf []byte
	buf = append(buf, fileType)
	for _, cat := range categories {
		// Inner message: field 1 (country_code string).
		var inner []byte
		inner = protowire.AppendTag(inner, 1, protowire.BytesType)
		inner = protowire.AppendString(inner, cat)
		// Outer: repeated field 1 containing the inner message.
		buf = protowire.AppendTag(buf, 1, protowire.BytesType)
		buf = protowire.AppendBytes(buf, inner)
	}
	return buf
}

func TestParseDatCategories_GeoSite(t *testing.T) {
	data := buildTestDat(0x01, []string{"google", "youtube", "netflix"})
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
	data := buildTestDat(0x02, []string{"ru", "cn", "us", "private"})
	result, err := parseDatCategories(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 categories, got %d: %v", len(result), result)
	}
}

func TestParseDatCategories_SingleCategory(t *testing.T) {
	data := buildTestDat(0x01, []string{"single"})
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

func TestParseDatCategories_OneByte(t *testing.T) {
	result, _ := parseDatCategories([]byte{0x01})
	if len(result) != 0 {
		t.Errorf("expected empty for type-byte-only, got %v", result)
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
	if err := os.WriteFile(geoSite, buildTestDat(0x01, []string{"google", "youtube"}), 0o644); err != nil {
		t.Fatal(err)
	}

	geoIP := filepath.Join(dir, "geoip.dat")
	if err := os.WriteFile(geoIP, buildTestDat(0x02, []string{"ru", "us"}), 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), buildTestDat(0x01, []string{"google"}), 0o644); err != nil {
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
