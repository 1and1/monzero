package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/scrypt"
)

type (
	pwHash struct {
		salt []byte
		hash []byte
	}
)

// Create a new password hash.
func newHash(pw string) (*pwHash, error) {
	hash := pwHash{}
	if err := hash.genSalt(); err != nil {
		return nil, err
	}
	h, err := hash.Hash(pw)
	if err != nil {
		return nil, err
	}
	hash.hash = h
	return &hash, nil
}

// generate a hash for the given salt and password
func (p *pwHash) Hash(pw string) ([]byte, error) {
	if len(p.salt) == 0 {
		return []byte{}, fmt.Errorf("salt not initialized")
	}
	// constants taken from https://godoc.org/golang.org/x/crypto/scrypt
	hash, err := scrypt.Key([]byte(pw), p.salt, 32768, 8, 1, 32)
	if err != nil {
		return []byte{}, fmt.Errorf("could not compute hash: %s", err)
	}
	return hash, nil
}

// genSalt generates 8 bytes of salt.
func (p *pwHash) genSalt() error {
	salt := make([]byte, 8)
	_, err := rand.Read(salt)
	p.salt = salt
	return err
}

// compare a hash to a password and return true, when it matches.
func (p *pwHash) compare(pw string) (bool, error) {
	hash, err := p.Hash(pw)
	if err != nil {
		return false, fmt.Errorf("could not check password")
	}
	if bytes.Compare(p.hash, hash) == 0 {
		return true, nil
	}
	return false, nil
}

// Encode a hash and salt to a string.
func (p *pwHash) String() string {
	return fmt.Sprintf(
		"1$%s$%s",
		base64.StdEncoding.EncodeToString(p.salt),
		base64.StdEncoding.EncodeToString(p.hash),
	)
}

// Parse a hash from a file or anywhere.
func (p *pwHash) Parse(raw string) error {
	if len(raw) == 0 {
		return fmt.Errorf("no hash found")
	}
	parts := strings.Split(raw, "$")
	if len(parts) != 3 {
		return fmt.Errorf("format error")
	}
	if parts[0] != "1" {
		return fmt.Errorf("unknown hash version")
	}
	salt, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("could not parse salt: %s", err)
	}
	hash, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("could not parse salt: %s", err)
	}
	p.salt = salt
	p.hash = hash
	return nil
}
