package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/theinventor/footagepal-cli/internal/client"
	"github.com/theinventor/footagepal-cli/internal/config"
	"github.com/theinventor/footagepal-cli/internal/credstore"
	"github.com/theinventor/footagepal-cli/internal/exitcode"
)

const storageFlagDescription = "where to persist the API token: auto (default; keychain if available, else file), keychain, or file"

func newAuthCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "auth",
		Short: "Manage saved API token profiles",
		Long: `Manage FootagePal API token profiles.

Resolution order at command time:
  1. --profile <name>
  2. FOOTAGEPAL_API_TOKEN and optional FOOTAGEPAL_API_URL
  3. default profile in ~/.config/footagepal/config.json

Create or copy an API token in the FootagePal web app, then save it with
footagepal auth save --profile main --token fp_...`,
	}
	c.AddCommand(newAuthSaveCmd())
	c.AddCommand(newAuthStatusCmd())
	c.AddCommand(newAuthListCmd())
	c.AddCommand(newAuthUseCmd())
	c.AddCommand(newAuthLogoutCmd())
	c.AddCommand(newAuthMigrateCmd())
	return c
}

func resolveStorage(storage string) (string, error) {
	backend, err := credstore.ResolveBackend(storage)
	if err != nil {
		return "", exitcode.Wrap(exitcode.Usage, err)
	}
	return backend, nil
}

func persistProfile(name string, p config.Profile, token, backend string) (string, error) {
	canon, err := credstore.Put(name, backend, token)
	if err != nil {
		return "", err
	}
	switch canon {
	case credstore.BackendKeychain:
		p.APIToken = ""
	case credstore.BackendFile:
		p.APIToken = token
	}
	p.Backend = canon

	f, err := config.Load()
	if err != nil {
		return "", err
	}
	f.Put(name, p)
	if err := f.Save(); err != nil {
		return "", err
	}
	return canon, nil
}

func newAuthSaveCmd() *cobra.Command {
	var profileName, token, apiURL, storage string
	var human bool
	c := &cobra.Command{
		Use:   "save",
		Short: "Save an existing FootagePal API token as a profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if profileName == "" || token == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--profile and --token are both required"))
			}
			if apiURL == "" {
				apiURL = client.DefaultAPIURL
			}
			apiURL = strings.TrimRight(apiURL, "/")
			backend, err := resolveStorage(storage)
			if err != nil {
				return err
			}
			canon, err := persistProfile(profileName, config.Profile{APIURL: apiURL}, token, backend)
			if err != nil {
				return err
			}
			f, _ := config.Load()
			out := cmd.OutOrStdout()
			rec := map[string]any{
				"profile":     profileName,
				"api_url":     apiURL,
				"token":       client.MaskSecret(token),
				"storage":     credstore.Describe(canon),
				"config_path": config.Path(),
				"is_default":  f != nil && f.DefaultProfile == profileName,
			}
			if human {
				fmt.Fprintf(out, "saved profile %q\n", profileName)
				fmt.Fprintf(out, "api_url: %s\n", apiURL)
				fmt.Fprintf(out, "token: %s\n", client.MaskSecret(token))
				fmt.Fprintf(out, "storage: %s\n", credstore.Describe(canon))
				fmt.Fprintf(out, "config: %s\n", config.Path())
				return nil
			}
			return printJSON(out, rec)
		},
	}
	c.Flags().StringVar(&profileName, "profile", "", "profile name (required)")
	c.Flags().StringVar(&token, "token", "", "API token (required)")
	c.Flags().StringVar(&apiURL, "api-url", "", "FootagePal API base URL (default: production)")
	c.Flags().StringVar(&storage, "storage", "", storageFlagDescription)
	c.Flags().BoolVar(&human, "human", false, "render as human-readable text instead of JSON")
	return c
}

func newAuthStatusCmd() *cobra.Command {
	var profile, storageProbe string
	var human bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Show the credentials the CLI will use",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = storageProbe
			activeProfile := profile
			if activeProfile == "" {
				activeProfile = rootProfile
			}
			cli := client.NewWithProfile(activeProfile)
			cli.Version = Version

			rec := map[string]any{
				"api_url":     cli.BaseURL,
				"token":       cli.MaskedAPIToken(),
				"source":      nil,
				"backend":     credstore.Describe(cli.Backend),
				"cli_version": Version,
				"config_path": config.Path(),
			}
			if cli.Source != "" {
				rec["source"] = cli.Source
			}
			if strings.HasPrefix(cli.Source, "profile:") {
				rec["profile"] = strings.TrimPrefix(cli.Source, "profile:")
			}

			resp, err := cli.Do(http.MethodGet, "/api/v1/me", nil, nil)
			if err != nil {
				rec["reachable"] = false
				rec["reachable_error"] = client.RedactURL(err.Error())
			} else {
				rec["server_status"] = resp.StatusCode
				rec["reachable"] = resp.StatusCode >= 200 && resp.StatusCode < 500
				_ = resp.Body.Close()
			}

			out := cmd.OutOrStdout()
			if human {
				fmt.Fprintf(out, "api_url: %s\n", rec["api_url"])
				fmt.Fprintf(out, "token: %s\n", rec["token"])
				fmt.Fprintf(out, "source: %v\n", rec["source"])
				fmt.Fprintf(out, "backend: %s\n", rec["backend"])
				if status, ok := rec["server_status"]; ok {
					fmt.Fprintf(out, "server_status: %v\n", status)
				}
				return nil
			}
			return printJSON(out, rec)
		},
	}
	c.Flags().StringVar(&profile, "profile", "", "show status for a specific profile")
	c.Flags().StringVar(&storageProbe, "storage", "", "reserved for auth command consistency")
	c.Flags().BoolVar(&human, "human", false, "render as human-readable text instead of JSON")
	return c
}

func newAuthListCmd() *cobra.Command {
	var human bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List saved profiles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if human {
				rows := [][]string{}
				for _, name := range f.Names() {
					p := f.Profiles[name]
					defaultMarker := ""
					if name == f.DefaultProfile {
						defaultMarker = "*"
					}
					rows = append(rows, []string{name, defaultMarker, p.APIURL, credstore.Describe(p.Backend), client.MaskSecret(p.APIToken)})
				}
				printRows(out, []string{"PROFILE", "DEFAULT", "API_URL", "STORAGE", "TOKEN"}, rows)
				return nil
			}
			profiles := make([]map[string]any, 0, len(f.Profiles))
			for _, name := range f.Names() {
				p := f.Profiles[name]
				profiles = append(profiles, map[string]any{
					"name":       name,
					"api_url":    p.APIURL,
					"token":      client.MaskSecret(p.APIToken),
					"storage":    credstore.Describe(p.Backend),
					"is_default": name == f.DefaultProfile,
					"created_at": p.CreatedAt,
				})
			}
			return printJSON(out, map[string]any{
				"config_path":     config.Path(),
				"default_profile": f.DefaultProfile,
				"profiles":        profiles,
			})
		},
	}
	c.Flags().BoolVar(&human, "human", false, "render as a human-friendly table instead of JSON")
	return c
}

func newAuthUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile>",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := config.Load()
			if err != nil {
				return err
			}
			if err := f.SetDefault(args[0]); err != nil {
				return exitcode.Wrap(exitcode.NotFound, err)
			}
			if err := f.Save(); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), map[string]any{"default_profile": args[0]})
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	var profile string
	var force bool
	c := &cobra.Command{
		Use:   "logout",
		Short: "Remove a saved profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = force
			f, err := config.Load()
			if err != nil {
				return err
			}
			name := profile
			if name == "" {
				name = f.DefaultProfile
			}
			if name == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("no profile to remove"))
			}
			if !f.Delete(name) {
				return exitcode.Wrap(exitcode.NotFound, fmt.Errorf("no profile named %q", name))
			}
			if err := f.Save(); err != nil {
				return err
			}
			_ = credstore.Delete(name)
			return printJSON(cmd.OutOrStdout(), map[string]any{
				"removed_profile": name,
				"default_profile": f.DefaultProfile,
			})
		},
	}
	c.Flags().StringVar(&profile, "profile", "", "profile to remove (default: current default)")
	c.Flags().BoolVar(&force, "force", false, "reserved for future confirmation prompts")
	return c
}

func newAuthMigrateCmd() *cobra.Command {
	var profileName string
	var all bool
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Move file-backed profile tokens into the OS keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if profileName == "" && !all {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("pass --profile <name> or --all"))
			}
			if !credstore.KeychainAvailable() {
				return exitcode.Wrap(exitcode.Generic, errors.New("OS keychain unavailable; cannot migrate"))
			}
			f, err := config.Load()
			if err != nil {
				return err
			}
			targets := []string{}
			if all {
				targets = f.Names()
			} else {
				if _, ok := f.Get(profileName); !ok {
					return exitcode.Wrap(exitcode.NotFound, fmt.Errorf("no profile named %q", profileName))
				}
				targets = []string{profileName}
			}
			migrated := []string{}
			skipped := []string{}
			for _, name := range targets {
				p, _ := f.Get(name)
				if p.Backend == credstore.BackendKeychain {
					skipped = append(skipped, name)
					continue
				}
				if p.APIToken == "" {
					skipped = append(skipped, name)
					continue
				}
				if _, err := credstore.Put(name, credstore.BackendKeychain, p.APIToken); err != nil {
					return fmt.Errorf("migrate %s: %w", name, err)
				}
				p.APIToken = ""
				p.Backend = credstore.BackendKeychain
				f.Put(name, *p)
				migrated = append(migrated, name)
			}
			if len(migrated) > 0 {
				if err := f.Save(); err != nil {
					return err
				}
			}
			return printJSON(cmd.OutOrStdout(), map[string]any{
				"migrated": migrated,
				"skipped":  skipped,
			})
		},
	}
	c.Flags().StringVar(&profileName, "profile", "", "profile to migrate")
	c.Flags().BoolVar(&all, "all", false, "migrate every file-backed profile")
	return c
}
