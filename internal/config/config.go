package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Project struct {
	APIURL    string `json:"apiUrl"`
	ProjectID string `json:"projectId"`
}

const DefaultOrigin = "https://envault.strawhatdevs.com"

type User struct {
	APIURL string `json:"apiUrl"`
}

func NormalizeURL(raw string) (string, error) {
	if err := ValidateURL(raw); err != nil {
		return "", err
	}
	u, _ := url.Parse(raw)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "443" && u.Scheme == "https" || port == "80" && u.Scheme == "http" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}

func ValidateURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || u.Path != "" && u.Path != "/" {
		return errors.New("API URL must be an HTTPS origin")
	}
	hostname := strings.ToLower(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (hostname == "127.0.0.1" || hostname == "localhost" || hostname == "::1")) {
		return errors.New("HTTPS is required except on localhost")
	}
	return nil
}
func Load() (Project, error) {
	c, _, err := Find()
	return c, err
}

// Find returns the nearest binding without changing the working directory.
func Find() (Project, string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return Project{}, "", err
	}
	for {
		path := filepath.Join(dir, ".envault.json")
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return Project{}, path, fmt.Errorf("%s must be a regular file", path)
			}
			var c Project
			b, err := os.ReadFile(path)
			if err == nil {
				err = json.Unmarshal(b, &c)
			}
			if err == nil {
				err = ValidateURL(c.APIURL)
			}
			if err == nil && c.ProjectID == "" {
				err = errors.New("projectId is required")
			}
			if err != nil {
				return c, path, fmt.Errorf("%s: %w", path, err)
			}
			return c, path, nil
		}
		if !os.IsNotExist(err) {
			return Project{}, path, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Project{}, "", os.ErrNotExist
		}
		dir = parent
	}
}

func userPath() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "envault", "config.json"), err
}

func LoadUser() (User, error) {
	var c User
	path, err := userPath()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err == nil {
		err = ValidateURL(c.APIURL)
	}
	if err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func SaveUser(origin string) error {
	origin, err := NormalizeURL(origin)
	if err != nil {
		return err
	}
	path, err := userPath()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return saveJSON(path, User{APIURL: origin}, true)
}

func Save(c Project) error {
	return SaveProject(".envault.json", c, false)
}

func SaveProject(path string, c Project, replace bool) error {
	origin, err := NormalizeURL(c.APIURL)
	if err != nil {
		return err
	}
	if c.ProjectID == "" {
		return errors.New("projectId is required")
	}
	c.APIURL = origin
	return saveJSON(path, c, replace)
}

func saveJSON(path string, value any, replace bool) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular configuration: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".envault-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(b, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if !replace {
		return os.Link(f.Name(), path)
	}
	return os.Rename(f.Name(), path)
}
func Pin(apiURL, id, fingerprint string) error {
	dir, e := os.UserConfigDir()
	if e != nil {
		return e
	}
	dir = filepath.Join(dir, "envault", "fingerprints")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	safe := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(apiURL + "_" + id)
	path := filepath.Join(dir, safe)
	existing, e := os.ReadFile(path)
	if e == nil {
		if string(existing) != fingerprint {
			return errors.New("recipient fingerprint changed; verify identity before removing the old fingerprint pin")
		}
		return nil
	}
	if !os.IsNotExist(e) {
		return e
	}
	return os.WriteFile(path, []byte(fingerprint), 0600)
}

// Copy legacy pins before a normalized binding loses the original origin spelling.
func MigratePins(legacy, origin string) error {
	normalized, err := NormalizeURL(legacy)
	if err != nil || normalized != origin {
		return errors.New("cannot migrate pins between installations")
	}
	if legacy == origin {
		return nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, "envault", "fingerprints")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	prefix := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(legacy + "_")
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		if !entry.Type().IsRegular() {
			return errors.New("legacy fingerprint pin must be a regular file")
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		if err := Pin(origin, strings.TrimPrefix(entry.Name(), prefix), string(data)); err != nil {
			return err
		}
	}
	return nil
}
