package utils

import (
	"encoding/json"
    "strings"
)

// JSONCtoJSON converts JSON with comments to standard JSON.
// It strips single-line comments (//), multi-line comments (/* */),
// and trailing comments while preserving strings and handling escape sequences.
func JSONCtoJSON(data []byte) ([]byte, error) {
    str := string(data)
    var result strings.Builder
    inString := false
    inSingleComment := false
    inMultiComment := false
    escape := false

    for i := 0; i < len(str); i++ {
        c := str[i]

        // Handle escape sequences in strings
        if escape {
            escape = false
            if !inSingleComment && !inMultiComment {
                result.WriteByte(c)
            }
            continue
        }

        if c == '\\' && inString {
            escape = true
            if !inSingleComment && !inMultiComment {
                result.WriteByte(c)
            }
            continue
        }

        // Handle string boundaries
        if c == '"' && !inSingleComment && !inMultiComment {
            inString = !inString
            result.WriteByte(c)
            continue
        }

        // Skip processing if inside string
        if inString {
            result.WriteByte(c)
            continue
        }

        // Check for comment starts
        if !inSingleComment && !inMultiComment && i+1 < len(str) {
            if c == '/' && str[i+1] == '/' {
                inSingleComment = true
                i++ // skip next /
                continue
            }
            if c == '/' && str[i+1] == '*' {
                inMultiComment = true
                i++ // skip next *
                continue
            }
        }

        // Check for comment ends
        if inSingleComment && (c == '\n') {
            inSingleComment = false
            result.WriteByte(c) // keep newline for line numbers
            continue
        }

        if inMultiComment && i+1 < len(str) && c == '*' && str[i+1] == '/' {
            inMultiComment = false
            i++ // skip next /
            continue
        }

        // Write non-comment characters
        if !inSingleComment && !inMultiComment {
            result.WriteByte(c)
        }
    }

    // If we ended while in a single-line comment (no newline at EOF),
    // that's fine - the comment just ends at EOF

    return []byte(result.String()), nil
}

// ParseJSONC parses JSONC data into a generic structure.
// First converts JSONC to standard JSON, then unmarshals.
func ParseJSONC(data []byte) (interface{}, error) {
    jsonData, err := JSONCtoJSON(data)
    if err != nil {
        return nil, err
    }

    var result interface{}
    if err := json.Unmarshal(jsonData, &result); err != nil {
        return nil, err
    }

    return result, nil
}
