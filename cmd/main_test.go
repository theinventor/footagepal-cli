package cmd

import (
	"os"
	"testing"

	"github.com/theinventor/footagepal-cli/internal/credstore"
)

func TestMain(m *testing.M) {
	_, restore := credstore.UseMockKeyring()
	defer restore()
	os.Exit(m.Run())
}
