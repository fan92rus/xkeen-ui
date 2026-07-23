package happ

import (
	"encoding/json"
	"fmt"
	"log"
)

// ProxyEntry is a parsed proxy entry from a HAPP subscription response.
// The caller (subscription package) converts this to its own ProxyEntry type.
type ProxyEntry struct {
	Protocol    string          // "vless", "hysteria2"
	Fingerprint string          // TLS fingerprint
	TLSSecurity string          // "none", "tls", "reality"
	Network     string          // "tcp", "ws", "grpc", "hysteria"
	Outbound    json.RawMessage // full xray-compatible outbound JSON
	Tag         string          // server name + outbound tag
	Remarks     string          // server display name
	Country     string          // 2-letter country code from emoji flag
}

// Server represents a single server entry in the HAPP subscription response
// (sing-box JSON format). Exported so the subscription package can inspect
// the raw response for diagnostics.
type Server struct {
	Remarks   string          `json:"remarks"`
	Inbounds  json.RawMessage `json:"inbounds"`
	Outbounds json.RawMessage `json:"outbounds"`
	Routing   json.RawMessage `json:"routing"`
	DNS       json.RawMessage `json:"dns"`
	Stats     json.RawMessage `json:"stats"`
}

type singBoxOutbound struct {
	Protocol       string          `json:"protocol"`
	Tag            string          `json:"tag"`
	Settings       json.RawMessage `json:"settings"`
	StreamSettings json.RawMessage `json:"streamSettings"`
}

type streamSettings struct {
	Network  string `json:"network"`
	Security string `json:"security"`
	TLS      struct {
		Fingerprint string `json:"fingerprint"`
	} `json:"tlsSettings"`
	Reality struct {
		Fingerprint string `json:"fingerprint"`
	} `json:"realitySettings"`
	Hysteria struct {
		Auth    string `json:"auth"`
		Version int    `json:"version"`
	} `json:"hysteriaSettings"`
}

type vlessSettings struct {
	Vnext []struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
		Users   []struct {
			ID         string `json:"id"`
			Encryption string `json:"encryption"`
			Flow       string `json:"flow"`
		} `json:"users"`
	} `json:"vnext"`
}

type hysteriaSettings struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

// ConvertServer parses one sing-box Server and returns recognized outbounds
// as ProxyEntry values.
func ConvertServer(srv *Server) []*ProxyEntry {
	if srv == nil || len(srv.Outbounds) == 0 {
		return nil
	}

	var outbounds []json.RawMessage
	if err := json.Unmarshal(srv.Outbounds, &outbounds); err != nil {
		return nil
	}

	entries := make([]*ProxyEntry, 0, len(outbounds))
	for _, obRaw := range outbounds {
		var ob singBoxOutbound
		if err := json.Unmarshal(obRaw, &ob); err != nil {
			continue
		}

		pe, err := toProxyEntry(&ob, srv.Remarks)
		if err != nil {
			continue
		}
		entries = append(entries, pe)
	}
	return entries
}

// ConvertAllServers converts all servers and flattens recognized outbounds
// into a single list.
func ConvertAllServers(servers []Server) []*ProxyEntry {
	var all []*ProxyEntry
	for i := range servers {
		all = append(all, ConvertServer(&servers[i])...)
	}
	return all
}

func toProxyEntry(ob *singBoxOutbound, remarks string) (*ProxyEntry, error) {
	switch ob.Protocol {
	case "vless":
		return vlessToEntry(ob, remarks)
	case "hysteria":
		return hysteriaToEntry(ob, remarks)
	default:
		return nil, fmt.Errorf("happ: unsupported protocol %q", ob.Protocol)
	}
}

func parseSS(ob *singBoxOutbound) (network, sec, fp string) {
	if ob.StreamSettings == nil {
		return "", "none", ""
	}
	var ss streamSettings
	if err := json.Unmarshal(ob.StreamSettings, &ss); err != nil {
		return "", "none", ""
	}
	network = ss.Network
	sec = ss.Security
	if sec == "" {
		sec = "none"
	}
	if ss.TLS.Fingerprint != "" {
		fp = ss.TLS.Fingerprint
	} else if ss.Reality.Fingerprint != "" {
		fp = ss.Reality.Fingerprint
	}
	return
}

func vlessToEntry(ob *singBoxOutbound, remarks string) (*ProxyEntry, error) {
	var s vlessSettings
	if err := json.Unmarshal(ob.Settings, &s); err != nil || len(s.Vnext) == 0 {
		return nil, fmt.Errorf("happ: invalid vless settings")
	}

	network, sec, fp := parseSS(ob)

	// Reconstruct the full outbound JSON for xray compatibility
	out := ob.RawSettings()

	tag := ob.Tag
	if remarks != "" {
		tag = fmt.Sprintf("%s (%s)", remarks, tag)
	}

	return &ProxyEntry{
		Protocol:    "vless",
		Fingerprint: fp,
		TLSSecurity: sec,
		Network:     network,
		Outbound:    out,
		Tag:         tag,
		Remarks:     remarks,
		Country:     extractCountry(remarks),
	}, nil
}

func hysteriaToEntry(ob *singBoxOutbound, remarks string) (*ProxyEntry, error) {
	var s hysteriaSettings
	if err := json.Unmarshal(ob.Settings, &s); err != nil || s.Address == "" {
		return nil, fmt.Errorf("happ: invalid hysteria settings")
	}

	network, sec, fp := parseSS(ob)
	if sec == "none" {
		sec = "tls"
	}
	if network == "" {
		network = "hysteria"
	}

	out := ob.RawSettings()

	tag := ob.Tag
	if remarks != "" {
		tag = fmt.Sprintf("%s (%s)", remarks, tag)
	}

	return &ProxyEntry{
		Protocol:    "hysteria2",
		Fingerprint: fp,
		TLSSecurity: sec,
		Network:     network,
		Outbound:    out,
		Tag:         tag,
		Remarks:     remarks,
		Country:     extractCountry(remarks),
	}, nil
}

// RawSettings reconstructs the full xray-compatible outbound JSON from
// the sing-box outbound fields.
func (ob *singBoxOutbound) RawSettings() json.RawMessage {
	m := make(map[string]interface{})
	m["protocol"] = ob.Protocol
	if len(ob.Settings) > 0 {
		var v interface{}
		if err := json.Unmarshal(ob.Settings, &v); err == nil {
			m["settings"] = v
		}
	}
	if len(ob.StreamSettings) > 0 {
		var v interface{}
		if err := json.Unmarshal(ob.StreamSettings, &v); err == nil {
			m["streamSettings"] = v
		}
	}
	if ob.Tag != "" {
		m["tag"] = ob.Tag
	}
	// Add mux
	m["mux"] = map[string]interface{}{
		"enabled":         true,
		"concurrency":     -1,
		"xudpConcurrency": 16,
		"xudpProxyUDP443": "reject",
	}
	data, err := json.Marshal(m)
	if err != nil {
		log.Printf("happ: RawSettings marshal error: %v", err)
		return nil
	}
	return data
}

func extractCountry(remarks string) string {
	runes := []rune(remarks)
	for i := 0; i < len(runes)-1; i++ {
		if isRI(runes[i]) && isRI(runes[i+1]) {
			return string(runes[i]-0x1F1E6+'A') + string(runes[i+1]-0x1F1E6+'A')
		}
	}
	return ""
}

func isRI(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}
