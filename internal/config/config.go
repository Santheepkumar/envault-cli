package config

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Project struct {
	APIURL    string `json:"apiUrl"`
	ProjectID string `json:"projectId"`
}

func ValidateURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("API URL must be an HTTPS origin")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return errors.New("HTTPS is required except on localhost")
	}
	return nil
}
func Load() (Project, error) {
	var c Project
	b, e := os.ReadFile(".envault.json")
	if e != nil {
		return c, errors.New("run envault init or provide --project and --api-url")
	}
	e = json.Unmarshal(b, &c)
	if e != nil {
		return c, e
	}
	return c, ValidateURL(c.APIURL)
}
func Save(c Project) error {
	if e := ValidateURL(c.APIURL); e != nil {
		return e
	}
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(".envault.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = f.Write(append(b, '\n'))
	return e
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
