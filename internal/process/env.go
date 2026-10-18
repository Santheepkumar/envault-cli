package process

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseEnv never expands variables, substitutes commands, or invokes a shell.
func ParseEnv(data []byte) (map[string]string, error) {
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return nil, errors.New(".env must be UTF-8 without NUL bytes")
	}
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	out := map[string]string{}
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
			i++
		}
		if i == len(s) {
			break
		}
		if s[i] == '#' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if strings.HasPrefix(s[i:], "export ") {
			i += 7
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
		}
		start := i
		for i < len(s) && s[i] != '=' && s[i] != '\n' {
			i++
		}
		if i == len(s) || s[i] != '=' {
			return nil, errors.New("expected NAME=value")
		}
		key := strings.TrimSpace(s[start:i])
		if !identifier.MatchString(key) {
			return nil, fmt.Errorf("invalid variable name %q", key)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate variable %s", key)
		}
		i++
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		var value strings.Builder
		if i < len(s) && (s[i] == '\'' || s[i] == '"') {
			quote := s[i]
			i++
			closed := false
			for i < len(s) {
				c := s[i]
				i++
				if c == quote {
					closed = true
					break
				}
				if quote == '"' && c == '\\' && i < len(s) {
					next := s[i]
					i++
					switch next {
					case 'n':
						value.WriteByte('\n')
					case 'r':
						value.WriteByte('\r')
					case 't':
						value.WriteByte('\t')
					case '\\', '"':
						value.WriteByte(next)
					default:
						value.WriteByte('\\')
						value.WriteByte(next)
					}
				} else {
					value.WriteByte(c)
				}
			}
			if !closed {
				return nil, errors.New("unterminated quoted value")
			}
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			if i < len(s) && s[i] == '#' {
				for i < len(s) && s[i] != '\n' {
					i++
				}
			}
			if i < len(s) && s[i] != '\n' {
				return nil, errors.New("unexpected content after quoted value")
			}
		} else {
			start = i
			for i < len(s) && s[i] != '\n' {
				if s[i] == '#' && (i == start || s[i-1] == ' ' || s[i-1] == '\t') {
					break
				}
				i++
			}
			value.WriteString(strings.TrimRight(s[start:i], " \t"))
			for i < len(s) && s[i] != '\n' {
				i++
			}
		}
		out[key] = value.String()
	}
	return out, nil
}
