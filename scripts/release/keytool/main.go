// keytool creates offline release keys and signs checksums without logging keys.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"os"
)

func run(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: keytool generate PRIVATE_KEY_PATH | sign CHECKSUMS_PATH (EXCALIBUR_RELEASE_KEY contains private PEM)")
	}
	switch args[0] {
	case "generate":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		der, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(args[1], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = pem.Encode(file, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Println(hex.EncodeToString(pub))
		return nil
	case "sign":
		block, _ := pem.Decode([]byte(os.Getenv("EXCALIBUR_RELEASE_KEY")))
		if block == nil {
			return errors.New("EXCALIBUR_RELEASE_KEY must contain a PKCS8 PEM key")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return errors.New("invalid release key")
		}
		private, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return errors.New("release key must be ed25519")
		}
		if hex.EncodeToString(private.Public().(ed25519.PublicKey)) != daemon.ReleasePublicKey {
			return errors.New("release signing key does not match daemon.ReleasePublicKey")
		}
		raw, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		return os.WriteFile(args[1]+".sig", []byte(hex.EncodeToString(ed25519.Sign(private, raw))+"\n"), 0644)
	default:
		return errors.New("unknown keytool command")
	}
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
