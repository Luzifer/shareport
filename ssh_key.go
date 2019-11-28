package main

import (
	"crypto/x509"
	"encoding/pem"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

func signerFromPem(pemBytes []byte, password []byte) (ssh.Signer, error) {
	var err error

	// read pem block
	pemBlock, _ := pem.Decode(pemBytes)
	if pemBlock == nil {
		return nil, errors.New("Pem decode failed, no key found")
	}

	// handle encrypted key
	if x509.IsEncryptedPEMBlock(pemBlock) {
		// decrypt PEM
		pemBlock.Bytes, err = x509.DecryptPEMBlock(pemBlock, password)
		if err != nil {
			return nil, errors.Wrap(err, "Decrypting PEM block failed")
		}

		// get RSA, EC or DSA key
		key, err := parsePemBlock(pemBlock)
		if err != nil {
			return nil, err
		}

		// generate signer instance from key
		signer, err := ssh.NewSignerFromKey(key)
		if err != nil {
			return nil, errors.Wrap(err, "Creating signer from encrypted key failed")
		}

		return signer, nil
	}

	// generate signer instance from plain key
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, errors.Wrap(err, "Parsing plain private key failed")
	}

	return signer, nil
}

func parsePemBlock(block *pem.Block) (interface{}, error) {
	switch block.Type {

	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.Wrap(err, "Parsing PKCS private key failed")
		}
		return key, nil

	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.Wrap(err, "Parsing EC private key failed")
		}
		return key, nil

	case "DSA PRIVATE KEY":
		key, err := ssh.ParseDSAPrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.Wrap(err, "Parsing DSA private key failed")
		}
		return key, nil

	default:
		return nil, errors.Errorf("Parsing private key failed, unsupported key type %q", block.Type)

	}
}
