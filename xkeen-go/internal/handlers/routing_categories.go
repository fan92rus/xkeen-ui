package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// DatCategory describes a single category entry extracted from a .dat file.
type DatCategory struct {
	Name string `json:"name"`
	File string `json:"file"`
}

// GeoCategories holds category lists grouped by geosite/geoip.
type GeoCategories struct {
	GeoSite []DatCategory `json:"geosite"`
	GeoIP   []DatCategory `json:"geoip"`
}

// RoutingCategoriesHandler serves geosite/geoip category names extracted
// from V2Ray .dat files in the Xray config directory.
type RoutingCategoriesHandler struct {
	configDir string
}

// NewRoutingCategoriesHandler creates a handler that scans the given
// directory for .dat files and serves category names via HTTP GET.
func NewRoutingCategoriesHandler(configDir string) *RoutingCategoriesHandler {
	return &RoutingCategoriesHandler{configDir: configDir}
}

// ServeHTTP handles GET requests with a JSON list of geosite/geoip categories.
func (h *RoutingCategoriesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	categories, err := scanDatFiles(h.configDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(categories) //nolint:errchkjson // safe: struct always serializable
}

// scanDatFiles walks configDir for *.dat files, parses each one, and
// returns grouped category lists.
func scanDatFiles(configDir string) (*GeoCategories, error) {
	result := &GeoCategories{}
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return result, nil
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".dat") {
			continue
		}
		// Skip temporary files (e.g. interrupted downloads).
		if strings.HasSuffix(strings.ToLower(name), ".tmp") ||
			strings.Contains(name, ".tmp.") {
			continue
		}
		fullPath := filepath.Join(configDir, name)
		data, err := os.ReadFile(fullPath)
		if err != nil || len(data) == 0 {
			continue
		}
		categories, err := parseDatCategories(data)
		if err != nil || len(categories) == 0 {
			continue
		}
		// Classify by filename: "geoip" in name → GeoIP, otherwise GeoSite.
		isGeoIP := strings.Contains(strings.ToLower(name), "geoip")
		for _, cat := range categories {
			entry := DatCategory{Name: cat, File: name}
			if isGeoIP {
				result.GeoIP = append(result.GeoIP, entry)
			} else {
				result.GeoSite = append(result.GeoSite, entry)
			}
		}
	}
	return result, nil
}

// parseDatCategories reads a V2Ray .dat file (protobuf) and extracts
// category/region names from the outer repeated-field 1 entries.
//
// Shared structure for both GeoSiteList and GeoIPList:
//
//	repeated message {                 // field 1 (entry)
//	    string country_code = 1;       // field 1 — the name we extract
//	    repeated Domain/CIDR = 2;     // field 2 — skipped
//	}
func parseDatCategories(data []byte) ([]string, error) {
	if len(data) < 1 {
		return nil, nil
	}
	var result []string
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 || num != 1 {
			break
		}
		data = data[n:]
		if typ != protowire.BytesType {
			break
		}
		entryBytes, n := protowire.ConsumeBytes(data)
		if n < 0 {
			break
		}
		data = data[n:]
		entryData := entryBytes

		// Parse nested entry message — extract field 1 (country_code string).
		for len(entryData) > 0 {
			num2, _, n2 := protowire.ConsumeTag(entryData)
			if n2 < 0 {
				break
			}
			entryData = entryData[n2:]
			if num2 == 1 {
				s, n3 := protowire.ConsumeString(entryData)
				if n3 >= 0 {
					result = append(result, s)
					entryData = entryData[n3:]
				}
			} else {
				_, n3 := protowire.ConsumeBytes(entryData)
				if n3 < 0 {
					break
				}
				entryData = entryData[n3:]
			}
		}
	}
	return result, nil
}
