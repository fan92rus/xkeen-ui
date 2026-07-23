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

// ── Malformed protobuf edge cases ──

func TestParseDatCategories_Malformed_TruncatedEntry(t *testing.T) {
	inner := protowire.AppendTag(nil, 1, protowire.BytesType)
	inner = append(inner, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01)
	outer := protowire.AppendTag(nil, 1, protowire.BytesType)
	outer = protowire.AppendBytes(outer, inner)

	result, err := parseDatCategories(outer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty from truncated entry, got %v", result)
	}
}

func TestParseDatCategories_Malformed_RandomBytes(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}
	result, err := parseDatCategories(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty from random bytes, got %v", result)
	}
}

func TestParseDatCategories_Malformed_WrongWireType(t *testing.T) {
	data := protowire.AppendTag(nil, 1, protowire.VarintType)
	data = protowire.AppendVarint(data, 42)

	result, err := parseDatCategories(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty from wrong wire type, got %v", result)
	}
}

func TestParseDatCategories_Malformed_EmptyEntry(t *testing.T) {
	outer := protowire.AppendTag(nil, 1, protowire.BytesType)
	outer = protowire.AppendBytes(outer, []byte{})

	result, err := parseDatCategories(outer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty from empty entry, got %v", result)
	}
}

func TestParseDatCategories_Malformed_TruncatedOuter(t *testing.T) {
	data := protowire.AppendTag(nil, 1, protowire.BytesType)
	data = append(data, 0xFF)

	result, err := parseDatCategories(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty from truncated outer, got %v", result)
	}
}

func TestParseDatCategories_Field2BeforeField1(t *testing.T) {
	inner := protowire.AppendTag(nil, 2, protowire.BytesType)
	inner = protowire.AppendBytes(inner, []byte("domain-data"))
	inner = protowire.AppendTag(inner, 1, protowire.BytesType)
	inner = protowire.AppendString(inner, "my-category")

	outer := protowire.AppendTag(nil, 1, protowire.BytesType)
	outer = protowire.AppendBytes(outer, inner)

	result, err := parseDatCategories(outer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0] != "my-category" {
		t.Errorf("expected [my-category], got %v", result)
	}
}

// ── File size limit ──

func TestScanDatFiles_SkipsLargeFiles(t *testing.T) {
	dir := t.TempDir()
	buf := make([]byte, maxDatFileSize+1)
	if err := os.WriteFile(filepath.Join(dir, "huge.dat"), buf, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good.dat"), buildTestDat([]string{"ok"}), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := scanDatFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.GeoSite) != 1 {
		t.Errorf("expected 1 geosite entry from good.dat, got %d", len(result.GeoSite))
	}
}

// ── Handler cache ──

func TestRoutingCategoriesHandler_CacheServesFreshWithinTTL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), buildTestDat([]string{"initial"}), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := NewRoutingCategoriesHandler(dir)
	Handler := http.HandlerFunc(handler.ServeHTTP)

	// First request populates cache
	w1 := httptest.NewRecorder()
	Handler.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	if w1.Code != http.StatusOK {
		t.Fatalf("first: expected 200, got %d", w1.Code)
	}

	// Modify file while cache is still fresh (within 30s TTL)
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), buildTestDat([]string{"modified"}), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second request should serve CACHED "initial", not "modified"
	w2 := httptest.NewRecorder()
	Handler.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	if w2.Code != http.StatusOK {
		t.Fatalf("second: expected 200, got %d", w2.Code)
	}

	var geo1, geo2 GeoCategories
	json.NewDecoder(w1.Body).Decode(&geo1)
	json.NewDecoder(w2.Body).Decode(&geo2)
	if len(geo1.GeoSite) != 1 || len(geo2.GeoSite) != 1 {
		t.Fatal("expected 1 geosite entry in both")
	}
	if geo2.GeoSite[0].Name != "initial" {
		t.Errorf("expected cached 'initial', got %q", geo2.GeoSite[0].Name)
	}
}
