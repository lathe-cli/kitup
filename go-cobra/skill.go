package kitupcobra

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	kitup "github.com/lathe-cli/kitup/go"
	"github.com/spf13/cobra"
)

type Options struct {
	AppID        string
	SkillName    string
	Bundle       kitup.SkillBundle
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
	cmd.AddCommand(NewInstallCommand(opts), NewStatusCommand(opts), NewUninstallCommand(opts))
	return cmd
}

func NewStatusCommand(opts Options) *cobra.Command {
	scope := defaultScope(opts)
	var agents []string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Show bundled Agent Skill status",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed := kitup.ParseInstallFlags(kitup.InstallFlagValues{Scope: scope, ScopeSet: true, Agents: agents})
			if err := kitup.InstallFlagError(parsed.Errors); err != nil {
				return err
			}
			selected, err := lifecycleAgents(opts, parsed.Scope, parsed.Agents, len(agents) > 0)
			if err != nil {
				return err
			}
			report, err := kitup.StatusBundledSkill(kitup.StatusOptions{
				BaseOptions: baseOptions(opts),
				AppID:       opts.AppID,
				SkillName:   opts.SkillName,
				Scope:       parsed.Scope,
				Agents:      selected,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				if err := writeJSON(output(cmd, opts), report); err != nil {
					return err
				}
			} else {
				renderStatusReport(output(cmd, opts), report)
			}
			return statusReportError(report)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", scope, kitup.InstallUX.ScopeFlag)
	cmd.Flags().StringArrayVar(&agents, "agent", nil, kitup.InstallUX.AgentFlag)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Write a structured JSON report")
	return cmd
}

func NewUninstallCommand(opts Options) *cobra.Command {
	scope := defaultScope(opts)
	var agents []string
	var yes bool
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:          "uninstall",
		Short:        "Uninstall bundled Agent Skill",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed := kitup.ParseInstallFlags(kitup.InstallFlagValues{Scope: scope, ScopeSet: true, Agents: agents, Yes: yes})
			if err := kitup.InstallFlagError(parsed.Errors); err != nil {
				return err
			}
			in := input(cmd, opts)
			if !parsed.Yes && !isTTY(in, opts.StdinTTY) {
				return errors.New("kitup: uninstall requires --yes when stdin is not a TTY")
			}
			selected, err := lifecycleAgents(opts, parsed.Scope, parsed.Agents, len(agents) > 0)
			if err != nil {
				return err
			}
			status, err := kitup.StatusBundledSkill(kitup.StatusOptions{
				BaseOptions: baseOptions(opts),
				AppID:       opts.AppID,
				SkillName:   opts.SkillName,
				Scope:       parsed.Scope,
				Agents:      selected,
			})
			if err != nil {
				return err
			}
			if len(status.Conflicts)+len(status.Errors) > 0 {
				report := uninstallReportFromStatus(status)
				if jsonOutput {
					if err := writeJSON(output(cmd, opts), report); err != nil {
						return err
					}
				} else {
					renderUninstallReport(output(cmd, opts), report)
				}
				return errors.New("kitup: uninstall has conflicts")
			}
			if len(status.Installed) == 0 {
				report := uninstallReportFromStatus(status)
				if jsonOutput {
					return writeJSON(output(cmd, opts), report)
				}
				renderUninstallReport(output(cmd, opts), report)
				return nil
			}
			promptOut := output(cmd, opts)
			if jsonOutput {
				promptOut = errOutput(cmd, opts)
			}
			if !jsonOutput {
				renderStatusReport(promptOut, status)
			}
			if !parsed.Yes {
				confirmed, err := confirmUninstall(in, promptOut, len(status.Installed))
				if err != nil {
					return err
				}
				if !confirmed {
					_, _ = fmt.Fprintln(promptOut, "Uninstall canceled.")
					if jsonOutput {
						return writeJSON(output(cmd, opts), uninstallReportFromStatus(status))
					}
					return nil
				}
			}
			report, err := kitup.UninstallBundledSkill(kitup.UninstallOptions{
				BaseOptions: baseOptions(opts),
				AppID:       opts.AppID,
				SkillName:   opts.SkillName,
				Scope:       parsed.Scope,
				Agents:      selected,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				if err := writeJSON(output(cmd, opts), report); err != nil {
					return err
				}
			} else {
				renderUninstallReport(output(cmd, opts), report)
			}
			if len(report.Conflicts)+len(report.Errors) > 0 {
				return errors.New("kitup: uninstall failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", scope, kitup.InstallUX.ScopeFlag)
	cmd.Flags().StringArrayVar(&agents, "agent", nil, kitup.InstallUX.AgentFlag)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip uninstall confirmation")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Write a structured JSON report")
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

func baseOptions(opts Options) kitup.BaseOptions {
	return kitup.BaseOptions{Home: opts.Home, CWD: opts.CWD, HostsFile: opts.HostsFile}
}

func defaultScope(opts Options) string {
	if opts.DefaultScope == kitup.ProjectScope {
		return string(kitup.ProjectScope)
	}
	return string(kitup.UserScope)
}

func lifecycleAgents(opts Options, scope kitup.Scope, parsed kitup.AgentSelector, explicit bool) (kitup.AgentSelector, error) {
	if explicit || opts.CurrentAgent == "" {
		return parsed, nil
	}
	selection, err := kitup.ResolveInstallSelection(kitup.InstallSelectionOptions{
		BaseOptions:  baseOptions(opts),
		Scope:        scope,
		Agents:       kitup.AutoAgents(),
		Yes:          true,
		CurrentAgent: opts.CurrentAgent,
	})
	if err != nil {
		return kitup.AgentSelector{}, err
	}
	if len(selection.Errors) > 0 {
		return kitup.AgentSelector{}, errors.New("kitup: agent selection failed")
	}
	return kitup.ExplicitAgents(selection.SelectedHostIDs...), nil
}

func statusReportError(report kitup.StatusReport) error {
	if len(report.Conflicts) > 0 {
		return errors.New("kitup: status has conflicts")
	}
	if len(report.Errors) > 0 {
		return errors.New("kitup: status failed")
	}
	return nil
}

func uninstallReportFromStatus(status kitup.StatusReport) kitup.UninstallReport {
	report := kitup.UninstallReport{
		Removed:   []kitup.TargetResult{},
		Skipped:   []kitup.TargetStatus{},
		Conflicts: append([]kitup.TargetStatus{}, status.Conflicts...),
		Errors:    append([]kitup.ReportError{}, status.Errors...),
	}
	for _, target := range status.Missing {
		report.Skipped = append(report.Skipped, kitup.TargetStatus{TargetResult: target, Reason: "missing"})
	}
	return report
}

func confirmUninstall(in io.Reader, out io.Writer, count int) (bool, error) {
	if _, err := fmt.Fprintf(out, "Remove %d installed target(s)? [y/N] ", count); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func isTTY(in io.Reader, configured bool) bool {
	if configured {
		return true
	}
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func writeJSON(out io.Writer, value any) error {
	return json.NewEncoder(out).Encode(value)
}

func renderStatusReport(out io.Writer, report kitup.StatusReport) {
	for _, item := range report.Installed {
		_, _ = fmt.Fprintf(out, "installed\t%s\t%s\n", targetHosts(item.TargetResult), item.TargetDir)
	}
	for _, item := range report.Missing {
		_, _ = fmt.Fprintf(out, "missing\t%s\t%s\n", targetHosts(item), item.TargetDir)
	}
	for _, item := range report.Conflicts {
		_, _ = fmt.Fprintf(out, "conflict\t%s\t%s\t%s\n", targetHosts(item.TargetResult), item.TargetDir, item.Reason)
	}
	for _, item := range report.Errors {
		_, _ = fmt.Fprintf(out, "error\t%s\n", item.Reason)
	}
}

func renderUninstallReport(out io.Writer, report kitup.UninstallReport) {
	for _, item := range report.Removed {
		_, _ = fmt.Fprintf(out, "removed\t%s\t%s\n", targetHosts(item), item.TargetDir)
	}
	for _, item := range report.Skipped {
		_, _ = fmt.Fprintf(out, "skipped\t%s\t%s\t%s\n", targetHosts(item.TargetResult), item.TargetDir, item.Reason)
	}
	for _, item := range report.Conflicts {
		_, _ = fmt.Fprintf(out, "conflict\t%s\t%s\t%s\n", targetHosts(item.TargetResult), item.TargetDir, item.Reason)
	}
	for _, item := range report.Errors {
		_, _ = fmt.Fprintf(out, "error\t%s\n", item.Reason)
	}
}

func targetHosts(target kitup.TargetResult) string {
	if target.HostID != "" {
		return target.HostID
	}
	return strings.Join(target.HostIDs, ",")
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
