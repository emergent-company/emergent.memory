package integration

import (
	"os"
	"testing"

	framework "github.com/emergent-company/runlog"
)

func TestMain(m *testing.M) {
	framework.LoadDotEnv()
	os.Exit(m.Run())
}
