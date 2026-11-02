//go:build !linux

package platform

import (
	"errors"
	"testing"
)

func TestAccountOperationsUnsupported(t *testing.T) {
	if _, err := CurrentUserName(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := LocalTimezone(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatal(err)
	}
}
