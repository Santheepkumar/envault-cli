package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/Santheepkumar/envault-cli/internal/config"
	vcrypto "github.com/Santheepkumar/envault-cli/internal/crypto"
)

const recoveryKitLimit = 16 * 1024

type recoveryKit struct {
	V           int    `json:"v"`
	APIURL      string `json:"apiUrl"`
	UserID      string `json:"userId"`
	RecoveryKey string `json:"recoveryKey"`
}

// Validate the local kit before touching the keychain or making any request.
func readRecoveryKit(reader io.Reader, origin string) (recoveryKit, error) {
	var kit recoveryKit
	data, err := io.ReadAll(io.LimitReader(reader, recoveryKitLimit+1))
	defer vcrypto.Clear(data)
	if err != nil {
		return kit, err
	}
	if len(data) > recoveryKitLimit {
		return kit, fmt.Errorf("recovery kit exceeds the 16 KiB limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&kit) != nil || decoder.Decode(new(any)) != io.EOF || kit.V != 1 || !uuidCursor.MatchString(kit.UserID) {
		return recoveryKit{}, fmt.Errorf("invalid recovery kit schema")
	}
	if config.ValidateURL(kit.APIURL) != nil || config.ValidateURL(origin) != nil {
		return recoveryKit{}, fmt.Errorf("recovery kit must use a valid installation origin")
	}
	kitURL, _ := url.Parse(kit.APIURL)
	selectedURL, _ := url.Parse(origin)
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	if kitURL.Scheme != selectedURL.Scheme || !strings.EqualFold(kitURL.Hostname(), selectedURL.Hostname()) || port(kitURL) != port(selectedURL) {
		return recoveryKit{}, fmt.Errorf("recovery kit belongs to a different installation")
	}
	key, err := vcrypto.Decode(kit.RecoveryKey)
	defer vcrypto.Clear(key)
	if err != nil || len(key) != 32 || len(kit.RecoveryKey) != 44 || vcrypto.B64(key) != kit.RecoveryKey {
		return recoveryKit{}, fmt.Errorf("invalid recovery key")
	}
	return kit, nil
}
