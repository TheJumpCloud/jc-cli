package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/schema"
)

func newSchemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Machine-readable schema and command manifest",
		Long: `Return machine-readable JSON descriptions of JumpCloud resource types and CLI commands.

Output is always JSON (deterministic, machine-parseable). Schema data is generated
from command definitions (single source of truth).

Commands:
  jc schema resources           List all resource types
  jc schema commands            Full CLI command manifest
  jc schema <resource>          Schema for a specific resource (e.g. users, devices)

Examples:
  jc schema resources           List all resource types with API versions and verbs
  jc schema users               User schema with fields, types, and descriptions
  jc schema commands            Full command manifest with all flags and subcommands`,
	}

	cmd.AddCommand(newSchemaResourcesCmd())
	cmd.AddCommand(newSchemaCommandsCmd())

	// Add a catch-all RunE for dynamic resource names.
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return runSchemaResource(cmd, args[0])
	}
	// Accept arbitrary args for dynamic resource name resolution.
	cmd.Args = cobra.ArbitraryArgs

	// Provide completion for resource names as positional args.
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return schema.ResourceNames(), cobra.ShellCompDirectiveNoFileComp
	}

	return cmd
}

func newSchemaResourcesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resources",
		Short: "List all resource types",
		Long: `List all JumpCloud resource types as a JSON array.

Each entry includes the resource name, API version, available verbs,
default fields, field definitions with types, filter/sort support,
and ID/name field mappings.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeJSON(cmd, schema.AllResources())
		},
	}
}

func newSchemaCommandsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "Full CLI command manifest",
		Long: `Return the full CLI command manifest as JSON.

Includes all command groups, subcommands, flags (with types, defaults,
and descriptions), and global flags. This is a machine-readable
representation of the entire CLI for LLM consumption.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeJSON(cmd, schema.BuildCommandManifest())
		},
	}
}

// runSchemaResource handles "jc schema <resource>" for dynamic resource names.
func runSchemaResource(cmd *cobra.Command, name string) error {
	s := schema.GetResource(name)
	if s == nil {
		available := strings.Join(schema.ResourceNames(), ", ")
		return fmt.Errorf("unknown resource %q. Available resources: %s", name, available)
	}
	return writeJSON(cmd, s)
}

// writeJSON writes a value as pretty-printed JSON to the command's stdout.
func writeJSON(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}
