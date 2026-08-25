package kitupcobra

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	kitup "github.com/lathe-cli/kitup/go"
	"github.com/spf13/cobra"
)

type Options struct {
	AppID        string
	Bundle       kitup.SkillBundle
	SkillName    string
	DefaultScope kitup.Scope
	Home         string
	CWD          string
	HostsFile    string
	CurrentAgent string
	StdinTTY     bool
	In           io.Reader
	Out          io.Writer
	Err          io.Writer
}

func NewSkillCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:          kitup.InstallUX.SkillUse,
		Short:        kitup.InstallUX.SkillShort,
		SilenceUsage: true,
	}
	cmd.AddCommand(NewInstallCommand(opts))
	cmd.AddCommand(NewStatusCommand(opts))
	cmd.AddCommand(NewUninstallCommand(opts))
	return cmd
}

func NewInstallCommand(opts Options) *cobra.Command {
	scope := ""
	var agents []string
	var yes bool
	var dryRun bool
	var force bool

	cmd := &cobra.Command{
		Use:          kitup.InstallUX.InstallUse,
		Short:        kitup.InstallUX.InstallShort,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed := kitup.ParseInstallFlags(kitup.InstallFlagValues{
				Scope:    scope,
				ScopeSet: cmd.Flags().Changed("scope"),
				Agents:   agents,
				Yes:      yes,
				DryRun:   dryRun,
				Force:    force,
			})
			if err := kitup.InstallFlagError(parsed.Errors); err != nil {
				return err
			}
			report, err := kitup.RunBundledSkillInstall(kitup.InstallWorkflowOptions{
				InstallOptions: kitup.InstallOptions{
					BaseOptions: kitup.BaseOptions{
						Home:      opts.Home,
						CWD:       opts.CWD,
						HostsFile: opts.HostsFile,
					},
					AppID:       opts.AppID,
					SkillBundle: opts.Bundle,
					Scope:       parsed.Scope,
					Agents:      parsed.Agents,
					Force:       parsed.Force,
				},
				Yes:          parsed.Yes,
				DryRun:       parsed.DryRun,
				StdinTTY:     opts.StdinTTY,
				CurrentAgent: opts.CurrentAgent,
				DefaultScope: opts.DefaultScope,
				ScopeSet:     parsed.ScopeSet,
				PromptScope:  true,
				In:           input(cmd, opts),
				Out:          output(cmd, opts),
				Err:          errOutput(cmd, opts),
			})
			if err != nil {
				return err
			}
			return kitup.InstallWorkflowError(report)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", scope, kitup.InstallUX.ScopeFlag)
	cmd.Flags().StringArrayVar(&agents, "agent", nil, kitup.InstallUX.AgentFlag)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, kitup.InstallUX.DryRunFlag)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, kitup.InstallUX.YesFlag)
	cmd.Flags().BoolVar(&force, "force", false, kitup.InstallUX.ForceFlag)
	return cmd
}

func NewStatusCommand(opts Options) *cobra.Command {
	var scope string
	var agents []string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Show installed bundled Agent Skill metadata",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName, err := lifecycleSkillName(opts)
			if err != nil {
				return err
			}
			parsed, err := parseLifecycleFlags(scope, agents, opts)
			if err != nil {
				return err
			}
			report, err := kitup.StatusBundledSkill(kitup.StatusOptions{
				BaseOptions: baseOptions(opts),
				AppID:       opts.AppID,
				SkillName:   skillName,
				Scope:       parsed.Scope,
				Agents:      parsed.Agents,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				if err := json.NewEncoder(output(cmd, opts)).Encode(report); err != nil {
					return err
				}
			} else {
				renderStatus(output(cmd, opts), report)
			}
			if len(report.Conflicts)+len(report.Errors) > 0 {
				return errors.New("skill status has conflicts")
			}
			return nil
		},
	}
	addLifecycleFlags(cmd, &scope, &agents, &jsonOutput)
	return cmd
}

func NewUninstallCommand(opts Options) *cobra.Command {
	var scope string
	var agents []string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:          "uninstall",
		Short:        "Uninstall the bundled Agent Skill",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName, err := lifecycleSkillName(opts)
			if err != nil {
				return err
			}
			parsed, err := parseLifecycleFlags(scope, agents, opts)
			if err != nil {
				return err
			}
			report, err := kitup.UninstallBundledSkill(kitup.UninstallOptions{
				BaseOptions: baseOptions(opts),
				AppID:       opts.AppID,
				SkillName:   skillName,
				Scope:       parsed.Scope,
				Agents:      parsed.Agents,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				if err := json.NewEncoder(output(cmd, opts)).Encode(report); err != nil {
					return err
				}
			} else {
				renderUninstall(output(cmd, opts), report)
			}
			if len(report.Conflicts)+len(report.Errors) > 0 {
				return errors.New("skill uninstall has conflicts")
			}
			return nil
		},
	}
	addLifecycleFlags(cmd, &scope, &agents, &jsonOutput)
	return cmd
}

func addLifecycleFlags(cmd *cobra.Command, scope *string, agents *[]string, jsonOutput *bool) {
	cmd.Flags().StringVar(scope, "scope", "", kitup.InstallUX.ScopeFlag)
	cmd.Flags().StringArrayVar(agents, "agent", nil, kitup.InstallUX.AgentFlag)
	cmd.Flags().BoolVar(jsonOutput, "json", false, "Output structured JSON")
}

func parseLifecycleFlags(scope string, agents []string, opts Options) (kitup.ParsedInstallFlags, error) {
	if scope == "" && opts.DefaultScope != "" {
		scope = string(opts.DefaultScope)
	}
	parsed := kitup.ParseInstallFlags(kitup.InstallFlagValues{Scope: scope, Agents: agents})
	if err := kitup.InstallFlagError(parsed.Errors); err != nil {
		return parsed, err
	}
	if len(agents) == 0 && opts.CurrentAgent != "" {
		selection, err := kitup.ResolveInstallSelection(kitup.InstallSelectionOptions{
			BaseOptions:  baseOptions(opts),
			Scope:        parsed.Scope,
			Agents:       kitup.AutoAgents(),
			Yes:          true,
			CurrentAgent: opts.CurrentAgent,
		})
		if err != nil {
			return parsed, err
		}
		if len(selection.Errors) > 0 {
			return parsed, errors.New("invalid lifecycle agent selection")
		}
		parsed.Agents = kitup.ExplicitAgents(selection.SelectedHostIDs...)
	}
	return parsed, nil
}

func baseOptions(opts Options) kitup.BaseOptions {
	return kitup.BaseOptions{Home: opts.Home, CWD: opts.CWD, HostsFile: opts.HostsFile}
}

func lifecycleSkillName(opts Options) (string, error) {
	if opts.SkillName != "" {
		return opts.SkillName, nil
	}
	info := kitup.ValidateSkillBundle(opts.Bundle)
	if !info.Valid || info.SkillName == "" {
		return "", errors.New("invalid bundled skill")
	}
	return info.SkillName, nil
}

func renderStatus(out io.Writer, report kitup.StatusReport) {
	for _, target := range report.Installed {
		parts := []string{fmt.Sprintf("installed %s at %s", target.SkillName, target.TargetDir)}
		if target.Metadata.CLIVersion != "" {
			parts = append(parts, "cli-version="+target.Metadata.CLIVersion)
		}
		if target.Metadata.Revision != "" {
			parts = append(parts, "revision="+target.Metadata.Revision)
		}
		if target.Metadata.SourceID != "" {
			parts = append(parts, "source-id="+target.Metadata.SourceID)
		}
		fmt.Fprintln(out, strings.Join(parts, " "))
	}
	for _, target := range report.Missing {
		fmt.Fprintf(out, "missing %s at %s\n", target.SkillName, target.TargetDir)
	}
	for _, target := range report.Conflicts {
		fmt.Fprintf(out, "conflict %s at %s: %s\n", target.SkillName, target.TargetDir, target.Reason)
	}
	renderErrors(out, report.Errors)
}

func renderUninstall(out io.Writer, report kitup.UninstallReport) {
	for _, target := range report.Removed {
		fmt.Fprintf(out, "removed %s from %s\n", target.SkillName, target.TargetDir)
	}
	for _, target := range report.Skipped {
		fmt.Fprintf(out, "skipped %s at %s: %s\n", target.SkillName, target.TargetDir, target.Reason)
	}
	for _, target := range report.Conflicts {
		fmt.Fprintf(out, "conflict %s at %s: %s\n", target.SkillName, target.TargetDir, target.Reason)
	}
	renderErrors(out, report.Errors)
}

func renderErrors(out io.Writer, errors []kitup.ReportError) {
	for _, reportErr := range errors {
		context := reportErr.Agent
		if context == "" {
			context = reportErr.HostID
		}
		if context == "" {
			context = reportErr.SkillName
		}
		if context == "" {
			fmt.Fprintf(out, "error: %s\n", reportErr.Reason)
		} else {
			fmt.Fprintf(out, "error %s: %s\n", context, reportErr.Reason)
		}
	}
}

func input(cmd *cobra.Command, opts Options) io.Reader {
	if opts.In != nil {
		return opts.In
	}
	return cmd.InOrStdin()
}

func output(cmd *cobra.Command, opts Options) io.Writer {
	if opts.Out != nil {
		return opts.Out
	}
	return cmd.OutOrStdout()
}

func errOutput(cmd *cobra.Command, opts Options) io.Writer {
	if opts.Err != nil {
		return opts.Err
	}
	return cmd.ErrOrStderr()
}
