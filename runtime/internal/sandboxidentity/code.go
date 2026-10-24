// Package sandboxidentity manages a sandbox's persistent public identity and pairing code.
package sandboxidentity

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"molr.ai/runtime/internal/core"
)

const prefix = "molr-sandbox-v1."

type connectionCode struct {
	PublicKeySHA256 string `json:"public_key_sha256"`
	Token           string `json:"token"`
}

// PublicKeySHA256 identifies the SubjectPublicKeyInfo, independently of certificate renewal.
func PublicKeySHA256(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

// Encode returns a secret pairing code without a network address or release version.
func Encode(publicKey string, token core.Secret) (core.Secret, error) {
	value := connectionCode{PublicKeySHA256: publicKey, Token: token.Reveal()}
	if !valid(value) {
		return core.Secret{}, invalidCode()
	}
	data, err := json.Marshal(value)
	if err != nil {
		return core.Secret{}, invalidCode()
	}
	defer clear(data)
	return core.NewSecret(prefix + base64.RawURLEncoding.EncodeToString(data)), nil
}

// Decode validates the code format version before exposing its credentials to the caller.
func Decode(code string) (string, core.Secret, error) {
	code = strings.TrimSpace(code)
	if len(code) > 8192 {
		return "", core.Secret{}, invalidCode()
	}
	if !strings.HasPrefix(code, prefix) {
		if strings.HasPrefix(code, "molr-sandbox-v") {
			return "", core.Secret{}, core.WithReason(core.Invalid("connection_code", "unsupported connection code version"), "sandbox_connection_code_unsupported")
		}
		return "", core.Secret{}, invalidCode()
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(code, prefix))
	if err != nil {
		return "", core.Secret{}, invalidCode()
	}
	defer clear(data)
	var value connectionCode
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || !valid(value) {
		return "", core.Secret{}, invalidCode()
	}
	return value.PublicKeySHA256, core.NewSecret(value.Token), nil
}

func valid(value connectionCode) bool {
	pin, err := hex.DecodeString(value.PublicKeySHA256)
	return err == nil && len(pin) == sha256.Size && value.PublicKeySHA256 == strings.ToLower(value.PublicKeySHA256) && len(value.Token) >= 32 && len(value.Token) <= 4096 && !strings.ContainsAny(value.Token, " \t\r\n")
}
func invalidCode() error {
	return core.WithReason(core.Invalid("connection_code", "invalid sandbox connection code"), "sandbox_connection_code_invalid")
}
