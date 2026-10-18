package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
)

const MaxPlaintext = 1024 * 1024

type Box struct {
	V          int    `json:"v"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}
type KeyWrap struct {
	Box
	EphemeralPublicKey string `json:"ephemeralPublicKey"`
}
type KDF struct {
	Name        string `json:"name"`
	Memory      uint32 `json:"memory"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
}
type Account struct {
	V            int    `json:"v"`
	PublicKey    string `json:"publicKey"`
	Salt         string `json:"salt"`
	KDF          KDF    `json:"kdf"`
	PasswordWrap Box    `json:"passwordWrap"`
	RecoveryWrap Box    `json:"recoveryWrap"`
	PrivateKey   Box    `json:"privateKey"`
}
type Context struct {
	ProjectID   string `json:"projectId"`
	Environment string `json:"environment"`
	Epoch       int    `json:"epoch"`
}
type Session struct {
	Account    Account
	PrivateKey []byte
	VaultKey   []byte
}

func B64(b []byte) string             { return base64.StdEncoding.EncodeToString(b) }
func Decode(s string) ([]byte, error) { return base64.StdEncoding.Strict().DecodeString(s) }
func RandomKey() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func Clear(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
func PublicKey(k []byte) ([]byte, error) {
	p, e := ecdh.X25519().NewPrivateKey(k)
	if e != nil {
		return nil, e
	}
	return p.PublicKey().Bytes(), nil
}
func KeyContext(c Context) string {
	return fmt.Sprintf("envault:v1:key:%s:%s:%d", c.ProjectID, c.Environment, c.Epoch)
}
func SecretContext(c Context, revision string) string {
	return fmt.Sprintf("envault:v1:secret:%s:%s:%d:%s", c.ProjectID, c.Environment, c.Epoch, revision)
}
func WrapContext(c Context, recipient string) string {
	return KeyContext(c) + ":recipient:" + recipient
}
func DeriveKey(password string, salt []byte, kdf KDF) ([]byte, error) {
	if len(salt) != 16 || password == "" || kdf.Name != "argon2id" || kdf.Memory != 65536 || kdf.Iterations != 3 || kdf.Parallelism != 4 {
		return nil, errors.New("unsupported KDF or salt")
	}
	return argon2.IDKey([]byte(password), salt, kdf.Iterations, kdf.Memory, kdf.Parallelism, 32), nil
}
func Seal(plaintext, key []byte, context string) (Box, error) {
	if len(key) != 32 || len(plaintext) > MaxPlaintext {
		return Box{}, errors.New("invalid key or payload size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return Box{}, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return Box{}, err
	}
	nonce := make([]byte, 12)
	if _, err = rand.Read(nonce); err != nil {
		return Box{}, err
	}
	return Box{1, B64(nonce), B64(g.Seal(nil, nonce, plaintext, []byte(context)))}, nil
}
func Open(b Box, key []byte, context string) ([]byte, error) {
	if b.V != 1 || len(key) != 32 || len(b.Ciphertext) > 1400000 {
		return nil, errors.New("invalid encryption envelope")
	}
	nonce, e := Decode(b.Nonce)
	if e != nil || len(nonce) != 12 {
		return nil, errors.New("invalid nonce")
	}
	ct, e := Decode(b.Ciphertext)
	if e != nil || len(ct) < 16 || len(ct) > MaxPlaintext+16 {
		return nil, errors.New("invalid ciphertext")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	p, e := g.Open(nil, nonce, ct, []byte(context))
	if e != nil {
		return nil, errors.New("decryption failed: wrong key or modified data")
	}
	return p, nil
}
func wrappingKey(private, public, ephemeral []byte, context string) ([]byte, error) {
	p, e := ecdh.X25519().NewPrivateKey(private)
	if e != nil {
		return nil, e
	}
	pub, e := ecdh.X25519().NewPublicKey(public)
	if e != nil {
		return nil, e
	}
	shared, e := p.ECDH(pub)
	if e != nil {
		return nil, e
	}
	defer Clear(shared)
	salt := sha256.Sum256(ephemeral)
	return hkdf.Key(sha256.New, shared, salt[:], context, 32)
}
func WrapKey(key []byte, recipientPublic string, c Context, recipient string) (KeyWrap, error) {
	pub, e := Decode(recipientPublic)
	if e != nil {
		return KeyWrap{}, e
	}
	ephemeral := RandomKey()
	defer Clear(ephemeral)
	epub, e := PublicKey(ephemeral)
	if e != nil {
		return KeyWrap{}, e
	}
	ctx := WrapContext(c, recipient)
	k, e := wrappingKey(ephemeral, pub, epub, ctx)
	if e != nil {
		return KeyWrap{}, e
	}
	defer Clear(k)
	box, e := Seal(key, k, ctx)
	return KeyWrap{box, B64(epub)}, e
}
func UnwrapKey(w KeyWrap, private []byte, c Context, recipient string) ([]byte, error) {
	pub, e := Decode(w.EphemeralPublicKey)
	if e != nil {
		return nil, e
	}
	ctx := WrapContext(c, recipient)
	k, e := wrappingKey(private, pub, pub, ctx)
	if e != nil {
		return nil, e
	}
	defer Clear(k)
	key, e := Open(w.Box, k, ctx)
	if e == nil && len(key) != 32 {
		return nil, errors.New("invalid environment key")
	}
	return key, e
}
func accountContext(id, purpose string) string { return "envault:v1:account:" + id + ":" + purpose }
func Unlock(id string, a Account, password string) (Session, error) {
	if a.V != 1 {
		return Session{}, errors.New("unsupported account")
	}
	salt, e := Decode(a.Salt)
	if e != nil {
		return Session{}, e
	}
	k, e := DeriveKey(password, salt, a.KDF)
	if e != nil {
		return Session{}, e
	}
	defer Clear(k)
	vault, e := Open(a.PasswordWrap, k, accountContext(id, "password"))
	if e != nil {
		return Session{}, e
	}
	private, e := Open(a.PrivateKey, vault, accountContext(id, "identity"))
	if e != nil {
		Clear(vault)
		return Session{}, e
	}
	pub, e := PublicKey(private)
	if e != nil || B64(pub) != a.PublicKey {
		Clear(vault)
		Clear(private)
		return Session{}, errors.New("identity mismatch")
	}
	return Session{a, private, vault}, nil
}
func Recover(id string, a Account, recovery []byte, password string) (Account, error) {
	if len(password) < 12 {
		return a, errors.New("use at least 12 characters")
	}
	vault, e := Open(a.RecoveryWrap, recovery, accountContext(id, "recovery"))
	if e != nil {
		return a, e
	}
	defer Clear(vault)
	private, e := Open(a.PrivateKey, vault, accountContext(id, "identity"))
	if e != nil {
		return a, e
	}
	defer Clear(private)
	pub, e := PublicKey(private)
	if e != nil || B64(pub) != a.PublicKey {
		return a, errors.New("identity mismatch")
	}
	salt := make([]byte, 16)
	if _, e = rand.Read(salt); e != nil {
		return a, e
	}
	k, e := DeriveKey(password, salt, a.KDF)
	if e != nil {
		return a, e
	}
	defer Clear(k)
	a.Salt = B64(salt)
	a.PasswordWrap, e = Seal(vault, k, accountContext(id, "password"))
	return a, e
}
func Fingerprint(public string) (string, error) {
	b, e := Decode(public)
	if e != nil || len(b) != 32 {
		return "", errors.New("invalid public key")
	}
	h := sha256.Sum256(b)
	s := fmt.Sprintf("%x", h)
	out := ""
	for i := 0; i < len(s); i += 4 {
		if i > 0 {
			out += " "
		}
		out += s[i : i+4]
	}
	return out, nil
}
