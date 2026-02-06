package main

import (
	"testing"

	_ "github.com/go-resty/resty/v2"
	_ "github.com/olekukonko/tablewriter"
	_ "github.com/spf13/cobra"
	_ "github.com/spf13/viper"
	_ "gopkg.in/yaml.v3"
)

func TestDependencyImports(t *testing.T) {
	t.Log("All core dependencies can be imported")
}
