// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"strings"
	"time"
)

// DefaultXKeenPath is the xkeen binary whose "-help" output is the source of
// truth for the interactive command whitelist. Exported so it can be overridden
// in tests or config.
const DefaultXKeenPath = "/opt/bin/xkeen"

// HelpTimeout is how long we wait for `xkeen -help` before giving up (and
// returning an empty command set).
const HelpTimeout = 5 * time.Second

// parseHelp parses the textual output of `xkeen -help` into a map of
// whitelisted commands keyed by their flag (e.g. "-start").
//
// The expected format (two-space category headers, indented "-flag  description"
// command lines) is:
//
//	Установка
//	        -i              Основной режим установки XKeen ...
//	        -io             OffLine установка XKeen
//
//	parseHelp is whitespace-tolerant (tabs or any number of spaces). The first
//	whitespace-delimited token of an indented line starting with "-" is the
//	flag; the remainder (trimmed) is the description. A non-empty indented line
//	that does NOT start with "-" updates the current category.
//
//	Lines without leading whitespace or without a description are ignored.
//	Descriptions containing a help example like "xkeen -i -toff" are safe: only
//	the leading token is taken as the flag.
func parseHelp(output string) map[string]CommandConfig {
	result := make(map[string]CommandConfig)
	category := ""

	for _, raw := range strings.Split(output, "\n") {
		// Only consider lines that are indented (command or sub-text).
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		// Category headers and command lines are both indented in xkeen's help.
		// A line with no leading whitespace is top-level boilerplate — skip.
		if raw == trimmed {
			continue
		}

		if strings.HasPrefix(trimmed, "-") {
			fields := strings.Fields(trimmed)
			flag := fields[0]
			// Description = everything after the flag token, trimmed.
			desc := strings.TrimSpace(strings.TrimPrefix(trimmed, flag))
			if desc == "" {
				continue // flag without a description — skip
			}
			result[flag] = CommandConfig{
				Cmd:         flag,
				Description: desc,
				Dangerous:   isDangerous(category, desc),
				Timeout:     CommandTimeout,
			}
			continue
		}

		// Otherwise it's a category header (indented, non-dash, e.g. "Установка").
		category = trimmed
	}

	return result
}

// isDangerous classifies a command as dangerous (destructive/system-altering)
// based on its help category and description.
//
// Rules:
//   - Category starts with "установ" (Установка) or "удал" (Удаление) → dangerous.
//     ("Переустановка" intentionally NOT matched — reinstallation is non-destructive.)
//   - Description contains "удал" (удалить) or "деинсталляц" (деинсталляция) → dangerous.
//
// All matching is case-insensitive. Category-based matching is a prefix check
// to avoid matching "переустановка"; description matching is a substring check.
func isDangerous(category, description string) bool {
	cat := strings.ToLower(category)
	if strings.HasPrefix(cat, "установ") || strings.HasPrefix(cat, "удал") {
		return true
	}
	desc := strings.ToLower(description)
	if strings.Contains(desc, "удал") || strings.Contains(desc, "деинсталляц") {
		return true
	}
	return false
}
