package subscription

import (
	"bufio"
	"os"
	"strings"
)

// AWGConfigSection holds parsed key-values from an AWG .conf file.
// AWG conf files use INI format with two section types:
//
//	[Interface] — local interface settings (PrivateKey, Address, ListenPort, etc.)
//	[Peer]      — remote peer settings (PublicKey, Endpoint, AllowedIPs, etc.)
type AWGConfigSection struct {
	Type    string            // "Interface" or "Peer"
	Values  map[string]string // key → value (last value wins for duplicate keys)
	Order   []string          // keys in order of first appearance
	Comment string            // last comment line preceding this section (e.g. "peer: phone")
}

// AWGConf holds the parsed structure of an AWG .conf file.
type AWGConf struct {
	Interface *AWGConfigSection
	Peers     []*AWGConfigSection
}

// ParseAWGConf reads and parses an AWG .conf file (INI format).
// Returns the [Interface] section and all [Peer] sections.
func ParseAWGConf(path string) (*AWGConf, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	result := &AWGConf{}
	var current *AWGConfigSection
	var lastComment string // tracks comment line(s) preceding a section

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Track comments (# peer: <label>, etc.)
		if strings.HasPrefix(line, "#") {
			c := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if c != "" {
				lastComment = c
			}
			continue
		}

		// Skip empty lines and ; comments (reset pending comment)
		if line == "" || strings.HasPrefix(line, ";") {
			lastComment = ""
			continue
		}

		// Section header
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			secType := strings.TrimSpace(line[1 : len(line)-1])
			current = &AWGConfigSection{
				Type:    secType,
				Values:  make(map[string]string),
				Comment: lastComment,
			}
			lastComment = "" // consume
			if secType == "Interface" {
				result.Interface = current
			} else {
				result.Peers = append(result.Peers, current)
			}
			continue
		}

		// Key = Value
		if current != nil {
			idx := strings.Index(line, "=")
			if idx < 0 {
				continue
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if _, exists := current.Values[key]; !exists {
				current.Order = append(current.Order, key)
			}
			current.Values[key] = val
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// DetectAWGRole determines whether a config is a server (inbound) or client (outbound).
//
// Server: [Interface] has ListenPort AND [Peer] has no Endpoint.
// Client: [Peer] has Endpoint (regardless of ListenPort).
//
// This covers the vast majority of real-world configs:
//   - WARP / VPN provider client: no ListenPort, Peer has Endpoint  → client
//   - Home access server:          ListenPort=443, Peer has no Endpoint → server
func DetectAWGRole(conf *AWGConf) AWGRole {
	if conf == nil || conf.Interface == nil {
		return AWGRoleClient
	}

	_, hasListenPort := conf.Interface.Values["ListenPort"]

	// Check if any peer has an Endpoint
	hasEndpoint := false
	for _, p := range conf.Peers {
		if _, ok := p.Values["Endpoint"]; ok {
			hasEndpoint = true
			break
		}
	}

	// Server: listens AND peers don't connect outbound
	if hasListenPort && !hasEndpoint {
		return AWGRoleServer
	}
	return AWGRoleClient
}

// GetListenPort extracts the ListenPort from [Interface], returns 0 if absent.
func (c *AWGConf) GetListenPort() int {
	if c.Interface == nil {
		return 0
	}
	val, ok := c.Interface.Values["ListenPort"]
	if !ok {
		return 0
	}
	var port int
	for _, ch := range val {
		if ch < '0' || ch > '9' {
			return 0
		}
		port = port*10 + int(ch-'0')
	}
	return port
}

// GetAddress extracts the Address from [Interface].
func (c *AWGConf) GetAddress() string {
	if c.Interface == nil {
		return ""
	}
	return c.Interface.Values["Address"]
}

// GetPrivateKey extracts the PrivateKey from [Interface].
func (c *AWGConf) GetPrivateKey() string {
	if c.Interface == nil {
		return ""
	}
	return c.Interface.Values["PrivateKey"]
}

// GetTunnelSubnet derives the tunnel subnet from peer AllowedIPs.
// For a server, peers have AllowedIPs like "10.8.0.2/32", "10.8.0.3/32".
// Returns the /24 network "10.8.0.0/24" or "" if undeterminable.
func (c *AWGConf) GetTunnelSubnet() string {
	for _, p := range c.Peers {
		allowed := p.Values["AllowedIPs"]
		if allowed == "" {
			continue
		}
		// Take first IP, convert /32 → /24
		first := strings.Split(allowed, ",")[0]
		first = strings.TrimSpace(first)
		parts := strings.Split(first, "/")
		if len(parts) < 1 {
			continue
		}
		ipParts := strings.Split(parts[0], ".")
		if len(ipParts) == 4 {
			return ipParts[0] + "." + ipParts[1] + "." + ipParts[2] + ".0/24"
		}
	}
	// Fallback: derive from Interface Address
	addr := c.GetAddress()
	if addr != "" {
		ipPart := strings.Split(addr, "/")[0]
		ipParts := strings.Split(ipPart, ".")
		if len(ipParts) == 4 {
			return ipParts[0] + "." + ipParts[1] + "." + ipParts[2] + ".0/24"
		}
	}
	return ""
}
