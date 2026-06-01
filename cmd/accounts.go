package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

func newAccountsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "accounts",
		Short: "Inspect FootagePal accounts available to the token",
	}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List accessible FootagePal accounts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cli := newAPIClient()
			resp, err := cli.Do(http.MethodGet, "/api/v1/accounts", nil, nil)
			if err != nil {
				return err
			}
			var body any
			if err := decodeAPIResponse(resp, &body); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), body)
		},
	})
	return c
}
