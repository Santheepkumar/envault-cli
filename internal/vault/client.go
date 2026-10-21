package vault

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Santheepkumar/envault-cli/internal/api"
	vcrypto "github.com/Santheepkumar/envault-cli/internal/crypto"
	"io"
	"os"
	"path/filepath"
)

type Version struct {
	vcrypto.Context
	RevisionID string      `json:"revisionId"`
	Version    int         `json:"version"`
	Box        vcrypto.Box `json:"box"`
	CreatedAt  string      `json:"createdAt"`
	CreatedBy  string      `json:"createdBy"`
}
type Environment struct {
	vcrypto.Context
	Head            *string         `json:"head"`
	AccessRevision  int             `json:"accessRevision"`
	RotationPending bool            `json:"rotationPending"`
	Wrap            vcrypto.KeyWrap `json:"wrap"`
}
type Client struct {
	API             *api.Client
	UserID          string
	PrivateKey      []byte
	PublicKey       string
	VaultKey        []byte
	Account         vcrypto.Account
	AccountRevision int
	ProjectID       string
	Environment     string
	MachineID       string
	BaseHead        *string
	BaseKnown       bool
	OverwriteRemote bool
}

func UUID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func (c *Client) Close()       { vcrypto.Clear(c.PrivateKey); vcrypto.Clear(c.VaultKey) }
func (c *Client) Path() string { return "/projects/" + c.ProjectID + "/environments/" + c.Environment }
func (c *Client) Recipient() string {
	if c.MachineID != "" {
		return c.MachineID
	}
	return c.UserID
}
func (c *Client) GetEnvironment(ctx context.Context, epoch int) (Environment, []byte, error) {
	var env Environment
	path := c.Path()
	if epoch > 0 {
		path += fmt.Sprintf("?epoch=%d", epoch)
	}
	e := c.API.Do(ctx, "GET", path, nil, &env)
	if e != nil {
		return env, nil, e
	}
	if env.ProjectID != c.ProjectID || env.Environment != c.Environment || env.Epoch < 1 || (epoch > 0 && env.Epoch != epoch) {
		return env, nil, errors.New("server returned a mismatching encryption context")
	}
	trusted := vcrypto.Context{ProjectID: c.ProjectID, Environment: c.Environment, Epoch: env.Epoch}
	key, e := vcrypto.UnwrapKey(env.Wrap, c.PrivateKey, trusted, c.Recipient())
	return env, key, e
}
func (c *Client) Read(ctx context.Context, revision string) ([]byte, *Version, error) {
	if revision == "" {
		revision = "latest"
	}
	var result struct {
		Version *Version `json:"version"`
	}
	if e := c.API.Do(ctx, "GET", c.Path()+"/versions/"+revision, nil, &result); e != nil {
		return nil, nil, e
	}
	if result.Version == nil {
		return nil, nil, nil
	}
	v := result.Version
	if v.ProjectID != c.ProjectID || v.Environment != c.Environment || v.Epoch < 1 || (revision != "latest" && v.RevisionID != revision) {
		return nil, nil, errors.New("server returned a mismatching secret context")
	}
	_, key, e := c.GetEnvironment(ctx, v.Epoch)
	if e != nil {
		return nil, nil, e
	}
	defer vcrypto.Clear(key)
	trusted := vcrypto.Context{ProjectID: c.ProjectID, Environment: c.Environment, Epoch: v.Epoch}
	plaintext, e := vcrypto.Open(v.Box, key, vcrypto.SecretContext(trusted, v.RevisionID))
	return plaintext, v, e
}
func (c *Client) Push(ctx context.Context, data []byte) (Version, error) {
	env, key, e := c.GetEnvironment(ctx, 0)
	if e != nil {
		return Version{}, e
	}
	defer vcrypto.Clear(key)
	if env.RotationPending {
		return Version{}, errors.New("key rotation required before writing")
	}
	expected := c.BaseHead
	if c.OverwriteRemote {
		expected = env.Head
	} else if !c.BaseKnown && env.Head != nil {
		return Version{}, errors.New("unknown local base revision; pull first or explicitly use --overwrite-remote")
	}
	revision := UUID()
	box, e := vcrypto.Seal(data, key, vcrypto.SecretContext(env.Context, revision))
	if e != nil {
		return Version{}, e
	}
	var result struct {
		Version Version `json:"version"`
	}
	e = c.API.Do(ctx, "POST", c.Path()+"/versions", map[string]any{"revisionId": revision, "epoch": env.Epoch, "expectedHead": expected, "idempotencyKey": UUID(), "box": box}, &result)
	if e == nil {
		c.BaseHead = &result.Version.RevisionID
		c.BaseKnown = true
	}
	return result.Version, e
}
func (c *Client) CreateProject(ctx context.Context, name string) (string, error) {
	id := UUID()
	wraps := map[string]vcrypto.KeyWrap{}
	for _, env := range []string{"dev", "staging", "prod"} {
		key := vcrypto.RandomKey()
		wrap, e := vcrypto.WrapKey(key, c.PublicKey, vcrypto.Context{ProjectID: id, Environment: env, Epoch: 1}, c.UserID)
		vcrypto.Clear(key)
		if e != nil {
			return "", e
		}
		wraps[env] = wrap
	}
	e := c.API.Do(ctx, "POST", "/projects", map[string]any{"id": id, "name": name, "wraps": wraps}, nil)
	return id, e
}
func ReadFile(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	data, e := io.ReadAll(io.LimitReader(f, vcrypto.MaxPlaintext+1))
	if e == nil && len(data) > vcrypto.MaxPlaintext {
		return nil, errors.New("file exceeds 1 MiB limit")
	}
	return data, e
}
func WriteFile(path string, data []byte, force bool) error {
	absolute, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	dir := filepath.Dir(absolute)
	resolved, e := filepath.EvalSymlinks(dir)
	if e != nil {
		return e
	}
	absolute = filepath.Join(resolved, filepath.Base(absolute))
	dir = resolved
	info, e := os.Lstat(absolute)
	if e == nil {
		if !info.Mode().IsRegular() {
			return errors.New("refusing non-regular output or symlink")
		}
		if !force {
			return errors.New("file exists; confirm overwrite with --force")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	f, e := protectedTemp(dir)
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if !force {
		return os.Link(tmp, absolute)
	}
	return replaceFile(tmp, absolute)
}

type MachineBundle struct {
	V           int    `json:"v"`
	APIURL      string `json:"apiUrl"`
	Token       string `json:"token"`
	PrivateKey  string `json:"privateKey"`
	ProjectID   string `json:"projectId"`
	Environment string `json:"environment"`
	ID          string `json:"id"`
}

func ParseMachine(raw string) (MachineBundle, error) {
	var b MachineBundle
	e := json.Unmarshal([]byte(raw), &b)
	if e != nil || b.V != 1 || b.Token == "" || b.ID == "" || b.ProjectID == "" {
		return b, errors.New("invalid machine credential bundle")
	}
	k, e := vcrypto.Decode(b.PrivateKey)
	if e != nil || len(k) != 32 {
		return b, errors.New("invalid machine key")
	}
	vcrypto.Clear(k)
	if b.Environment != "dev" && b.Environment != "staging" && b.Environment != "prod" {
		return b, errors.New("invalid machine environment")
	}
	return b, nil
}
