package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Fetch the FootagePal API identity for the active token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := newAPIClient()
			resp, err := c.Do(http.MethodGet, "/api/v1/me", nil, nil)
			if err != nil {
				return err
			}
			var body map[string]any
			if err := decodeAPIResponse(resp, &body); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), map[string]any{
				"api_url":     c.BaseURL,
				"token":       c.MaskedAPIToken(),
				"source":      sourceOrNil(c.Source),
				"cli_version": Version,
				"me":          body,
			})
		},
	}
}

func sourceOrNil(source string) any {
	if source == "" {
		return nil
	}
	return source
}
