package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Santheepkumar/envault-cli/internal/config"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL   string
	Token     string
	HTTP      *http.Client
	LoginHint string
}
type Error struct {
	Code    string
	Message string
	Status  int
	Hint    string
}

func (e *Error) Error() string {
	message := fmt.Sprintf("%s: %s", e.Code, e.Message)
	if e.Hint != "" {
		message += "; " + e.Hint
	}
	return message
}
func New(origin, token string) (*Client, error) {
	var e error
	if origin, e = config.NormalizeURL(origin); e != nil {
		return nil, e
	}
	return &Client{BaseURL: origin, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Do(ctx context.Context, method, path string, input, output any) error {
	var data []byte
	var e error
	if input != nil {
		data, e = json.Marshal(input)
		if e != nil {
			return e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, c.BaseURL+"/api/v1"+path, bytes.NewReader(data))
	if e != nil {
		return e
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "envault-cli/0.1.0")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	response, e := c.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	body, e := io.ReadAll(io.LimitReader(response.Body, 3_000_001))
	if e != nil {
		return e
	}
	if len(body) > 3_000_000 {
		return fmt.Errorf("response exceeds protocol limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var result struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &result) != nil {
			return fmt.Errorf("server returned HTTP %d", response.StatusCode)
		}
		hint := ""
		if response.StatusCode == http.StatusUnauthorized {
			hint = c.LoginHint
		}
		return &Error{Code: result.Error.Code, Message: result.Error.Message, Status: response.StatusCode, Hint: hint}
	}
	if output != nil {
		return json.Unmarshal(body, output)
	}
	return nil
}
