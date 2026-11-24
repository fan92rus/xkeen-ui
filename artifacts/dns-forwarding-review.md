# Code review: native DNS zone forwarding (xkeen-go)

Scope: `internal/handlers/dns_sync.go`, `dns_sync_test.go`, `internal/config/config.go`,
`internal/handlers/settings.go`, `main.go`, `internal/handlers/cli.go`. No source files were modified.

Checks run (all clean):

- `go test ./internal/handlers/ -run DNS -v` — 3 tests PASS
- `go vet ./...` — no findings
- `go test ./internal/...` — all packages PASS

## Findings

### HIGH — "All zones" does not forward anything; it only deletes zone rules
`internal/handlers/dns_sync.go:73-82, 84-89, 96-108`

`AWDNSForwardAllZones` makes `want` empty, so the stale-removal loop deletes every
zone-scoped `ip name-server` entry, and the add loop at :101 iterates over nothing.
No default server is added or changed. Claim in `internal/config/config.go:75-76`
("forwards all DNS (empty zone)") is inaccurate; the UI text
(`web/src/locales/en.json:148`) is closer to what the code does. With a default
name-server of `1.1.1.1`, turning on "All zones" sends *all* queries — including
`.local` — to `1.1.1.1`, so local names break. There is no way to configure the
intended upstream. Also, `discoverDNSUpstream()` is still called at :96 *after*
the deletions; if it fails (:97-99), all zone rules are gone, no add is attempted,
and `system configuration save` is skipped — a destructive half-applied state.

### HIGH — Stale removal has no ownership tracking and deletes user/ISP rules
`internal/handlers/dns_sync.go:54-69, 84-89`

Every entry with a non-empty `domain:` in `show ip name-server` is treated as ours
and removed if its zone is not in the configured list. There is no marker, record,
or check of the upstream. This will delete:
- manually configured `ip name-server <ip> corp.example` rules,
- automatically added conditional servers (e.g. ISP `domain:` entries),
- rules bound `on <interface>` (the removal command omits the interface, so it
  will likely fail — silently, since the result is discarded at :87).

On an upgrade this runs at every startup (`main.go:295-301`) with the default zone
list, so it is not limited to explicit user action.

### HIGH — Forwarding is enabled by default for existing installations
`internal/config/config.go:162-163, 201-204`; `main.go:295-301`

`LoadConfig` starts from `DefaultConfig()` and unmarshals over it. Old config.json
files have no `awg_dns_forward` / `awg_dns_zones` keys, so they inherit
`AWDNSForward: true` and `local,home,lan,internal`. The startup hook then mutates
the router's DNS configuration without any opt-in, pointing those zones at
whatever `discoverDNSUpstream()` returns. If that is a public/ISP resolver,
local name resolution silently breaks. `main.go:51-73` (`defaultConfigJSON`) has
the same omission and is unused dead code.

### MEDIUM — Upstream selection is implicit and unstable
`internal/handlers/dns_sync.go:25-45, 96`

`discoverDNSUpstream()` returns the *first* non-loopback server with empty domain.
Keenetic's list normally contains `127.0.0.1` plus an ISP/automatic DNS server;
the "first" may be a public resolver or DHCP-supplied server, and may change after
reboot or uplink failover. The resulting zone rules then point at a different
upstream on each startup, with no config override or warning.

### MEDIUM — Destructive ordering and ignored command errors
`internal/handlers/dns_sync.go:56, 87, 92, 103-108`; `internal/handlers/settings.go:521-537`

- Removals happen before discovery and before adds. Any later failure leaves the
  old rules deleted and partially replaced rules in the running config.
- Errors from every `no ip name-server` and from `system configuration save` are
  discarded. A failed removal or failed save is invisible.
- On add failure (:104-106), the function errors, but `UpdateAWGInterfaces` logs
  the error and still returns HTTP 200 / `ok`, while config.json was already
  saved with the new settings. UI/operator sees success.
- `system configuration save` runs unconditionally on every boot and every save
  (flash writes) even when nothing changed.

### MEDIUM — Monolithic PUT is backward-incompatible
`internal/handlers/settings.go:448-458, 512-523`

All DNS fields decode from JSON with zero values. Any older/cached client doing
`PUT /settings/awg-interfaces` without the new keys sets `AWDNSForward=false`,
`AWDNSForwardAllZones=false`, `AWDNSZones=nil`, triggering the stale-removal path
and deleting all zone rules.

### MEDIUM — Duplicate zone entries are not fully cleaned up
`internal/handlers/dns_sync.go:55, 64-65, 85-88`

`existing` is keyed by zone only. If Keenetic holds the same zone with two
upstreams, the last one overwrites the first; only one `no ip name-server` is
issued, leaving the other rule in place (which can shadow the new rule).

### MEDIUM — Tests would not catch a broken parser / several core behaviors
`internal/handlers/dns_sync_test.go:10-19, 21-54, 56-82`

- `TestDiscoverDNSUpstream` fixture (:14) has a zoned entry (`1.1.1.1/domain: local`)
  *after* two defaults. A parser that ignores `domain:` and returns the first
  non-loopback address still passes. Need a zoned-only fixture that must yield
  `""`, plus one with only a default and one with no servers / ndmc error.
- `TestSyncDNSForward_Zones` fixture (:30) uses the **same IP** (`10.0.0.1`) for
  the default and the stale zone. A parser that conflates empty/non-empty domain
  produces the same expected `no ip name-server 10.0.0.1 oldzone` command, so the
  bug is invisible. There is also no assertion that a bare default-server rule
  (`no ip name-server 10.0.0.1`) is never issued. Use different IPs.
- No tests at all for: AllZones mode, ownership/preservation of unknown zones,
  add failure, partial sync state, missing `ndmc`, duplicate zones, or real
  multi-field Keenetic output (`port:`, `global:`, `service:`, `interface:`).
- Current tests pass; they simply do not cover the failure modes above.

### LOW — Parsing note: no stale `addr` carry-over in the current code
`internal/handlers/dns_sync.go:33-43, 57-69`

For documented Keenetic output (`address:` followed by `domain:`), `addr` is reset
at :67 after every domain line, so empty-domain defaults are not inserted into
`existing`, and `current` in `discoverDNSUpstream` is overwritten at every new
`address:`. I could not reproduce a stale-address bug there. The parser is still
fragile: it requires a `domain:` line for every server and assumes `address:`
precedes `domain:`; the `show` error at :56 is discarded, and `domain` is not
reset at :60 (harmless with one domain per entry). `zoneRe` (:23) also accepts
zones with leading/trailing hyphens and underscores, which Keenetic may reject —
such a zone is silently dropped from `want` and its existing rule is removed.

### LOW — Misc
- `internal/handlers/settings.go:523`: `SyncDNSForward(h.cfg)` runs outside
  `cfgMu`, so concurrent PUTs can interleave ndmc operations.
- `main.go:127-132`: `dns-sync` always reads `installConfig`, ignoring the
  `-config` path supported by the server (`main.go:262-270`).
- `internal/handlers/dns_sync.go:109`: `mapKeys` order is random; status text
  (and log) zone order is nondeterministic.

## Suggested priorities

1. Make all zone changes explicit and transactional: detect upstream first, do a
   no-op when nothing changes, ignore errors only when deliberately best-effort,
   and surface failures to the caller/UI.
2. Track ownership of rules created by xkeen-ui (or only manage zones/upstreams
   the user explicitly configured) before deleting anything.
3. Decide the AllZones semantics explicitly (configure the empty-zone upstream,
   or document/validate that the current default must be the router DNS proxy).
4. Change `AWDNSForward` default to false (or gate the migration behind an
   explicit opt-in) for existing configs.
5. Strengthen tests with distinct IPs, zoned-only output, and failure injection.
