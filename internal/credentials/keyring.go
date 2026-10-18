package credentials

import (
	"fmt"
	"github.com/zalando/go-keyring"
)

const service = "EnVault"

func Get(origin string) (string, error) {
	v, e := keyring.Get(service, origin)
	if e != nil {
		return "", fmt.Errorf("cannot read OS keychain; run envault login and ensure your keychain/Secret Service is available: %w", e)
	}
	return v, nil
}
func Set(origin, token string) error {
	if e := keyring.Set(service, origin, token); e != nil {
		return fmt.Errorf("OS keychain unavailable; enable macOS Keychain, Windows Credential Manager or Linux Secret Service: %w", e)
	}
	return nil
}
func Delete(origin string) error {
	e := keyring.Delete(service, origin)
	if e == keyring.ErrNotFound {
		return nil
	}
	return e
}
