package kitupcobra

import (
	"io"

	kitup "github.com/samzong/kitup/go"
	"github.com/spf13/cobra"
)

type Options struct {
	AppID        string
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
	cmd.AddCommand(NewInstallCommand(opts))
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
