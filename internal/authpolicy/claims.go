// Package authpolicy evaluates explicit admission and role rules against
// claims from a cryptographically verified OIDC identity token.
package authpolicy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Rules use JSON Pointer paths. Every path must match; values at a path
// are alternatives. Only exact strings and arrays of strings match.
type Rules map[string][]string

func Parse(raw string) (Rules, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var rules Rules
	if len(raw) > 16384 {
		return nil, fmt.Errorf("claim rules exceed 16 KiB")
	}
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, fmt.Errorf("claim rules must be an object of JSON Pointer paths and nonempty string arrays: %w", err)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("claim rules must not be empty")
	}
	for path, values := range rules {
		if !strings.HasPrefix(path, "/") || path == "/" || len(values) == 0 {
			return nil, fmt.Errorf("invalid claim rule %q", path)
		}
		for _, segment := range strings.Split(path[1:], "/") {
			for i := 0; i < len(segment); i++ {
				if segment[i] == '~' {
					if i+1 >= len(segment) || (segment[i+1] != '0' && segment[i+1] != '1') {
						return nil, fmt.Errorf("invalid JSON Pointer %q", path)
					}
					i++
				}
			}
		}
		for _, value := range values {
			if value == "" {
				return nil, fmt.Errorf("claim values must not be empty")
			}
		}
	}
	return rules, nil
}

func (r Rules) Match(claims map[string]any) bool {
	if len(r) == 0 {
		return false
	}
	for path, allowed := range r {
		var value any = claims
		for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
			object, ok := value.(map[string]any)
			if !ok {
				return false
			}
			key := strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
			value = object[key]
		}
		matched := false
		matches := func(v string) bool {
			for _, a := range allowed {
				if v == a {
					return true
				}
			}
			return false
		}
		switch v := value.(type) {
		case string:
			matched = matches(v)
		case []any:
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return false
				}
				matched = matched || matches(s)
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// Value resolves an RFC 6901 pointer in a verified claim object.
func Value(claims map[string]any, path string) any {
 var value any = claims
 for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
  object, ok := value.(map[string]any); if !ok { return nil }
  key := strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
  value = object[key]
 }
 return value
}
