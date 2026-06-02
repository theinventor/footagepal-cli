package cmd

import (
	"runtime/debug"

	"github.com/spf13/cobra"
	"github.com/theinventor/footagepal-cli/internal/client"
)

var Version = "dev"

var rootProfile string

func init() {
	if Version != "dev" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
}

func NewRootCmd() *cobra.Command {
	rootProfile = ""
	root := &cobra.Command{
		Use:           "footagepal",
		Short:         "FootagePal CLI for authenticated media search, uploads, albums, and downloads",
		Long:          "footagepal manages employee media handoffs through the FootagePal JSON API using API tokens.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&rootProfile, "profile", "", "saved auth profile to use for this invocation")

	root.AddCommand(newAgentContextCmd())
	root.AddCommand(newAuthCmd())
	root.AddCommand(newWhoamiCmd())
	root.AddCommand(newAccountsCmd())
	root.AddCommand(newAlbumsCmd())
	root.AddCommand(newMediaCmd())

	return root
}

func newAPIClient() *client.Client {
	c := client.NewWithProfile(rootProfile)
	c.Version = Version
	return c
}
