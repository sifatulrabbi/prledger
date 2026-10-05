package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/prledger/internal/config"
)

func newConfigCmd(d Deps, g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Find or create the prledger config file",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "path",
			Short: "Print the config file prledger reads",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				path, err := configPath(d, g)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), path)
				if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
					fmt.Fprintln(cmd.ErrOrStderr(), "note: it does not exist yet; run `prledger config init` to create it")
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "init",
			Short: "Write a commented starter config file",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				path, err := configPath(d, g)
				if err != nil {
					return err
				}
				if err := config.Init(path); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
				return nil
			},
		},
	)
	return cmd
}
