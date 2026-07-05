package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "show or change settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showConfig()
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "print the effective config (default)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return showConfig()
			},
		},
		&cobra.Command{
			Use:   "get <key>",
			Short: "print one value",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := config.Load()
				if err != nil {
					return fail("%v", err)
				}
				m := configAsMap(cfg)
				value, ok := m[args[0]]
				if !ok {
					return fail("unknown key '%s'", args[0])
				}
				raw, _ := json.MarshalIndent(value, "", "  ")
				fmt.Println(string(raw))
				return nil
			},
		},
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "set a value (JSON or plain string)",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				value := config.ParseValue(args[1])
				if err := config.Validate(args[0], value); err != nil {
					return fail("%v", err)
				}
				if err := config.SetValue(args[0], value); err != nil {
					return fail("%v", err)
				}
				raw, _ := json.Marshal(value)
				fmt.Printf("csm: %s = %s\n", args[0], raw)
				return nil
			},
		},
		&cobra.Command{
			Use:   "unset <key>",
			Short: "reset a key to its default",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := config.Validate(args[0], config.DefaultFor(args[0])); err != nil {
					return fail("unknown key '%s'", args[0])
				}
				if err := config.UnsetValue(args[0]); err != nil {
					return fail("%v", err)
				}
				raw, _ := json.Marshal(config.DefaultFor(args[0]))
				fmt.Printf("csm: %s reset to default: %s\n", args[0], raw)
				return nil
			},
		},
		&cobra.Command{
			Use:   "path",
			Short: "print the config file path",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Println(config.ConfigPath())
				return nil
			},
		},
		&cobra.Command{
			Use:   "edit",
			Short: "open the config file in your editor",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				if _, err := config.EnsureInit(); err != nil {
					return fail("%v", err)
				}
				path := config.ConfigPath()
				editor := os.Getenv("EDITOR")
				switch {
				case editor != "":
					c := exec.Command(editor, path)
					c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
					return c.Run()
				case runtime.GOOS == "windows":
					return exec.Command("cmd", "/c", "start", "", path).Run()
				case runtime.GOOS == "darwin":
					return exec.Command("open", path).Run()
				default:
					c := exec.Command("vi", path)
					c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
					return c.Run()
				}
			},
		},
	)
	return cmd
}

func showConfig() error {
	if _, err := config.EnsureInit(); err != nil {
		return fail("%v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Printf("# %s\n%s\n", config.ConfigPath(), raw)
	return nil
}

func configAsMap(cfg config.Config) map[string]any {
	raw, _ := json.Marshal(cfg)
	m := map[string]any{}
	json.Unmarshal(raw, &m)
	return m
}
