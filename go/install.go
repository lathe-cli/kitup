package kitup

func InstallBundledSkill(opts InstallOptions) (InstallReport, error) {
	return installOrPlan(opts, true)
}

func PlanBundledSkill(opts InstallOptions) (InstallReport, error) {
	return installOrPlan(opts, false)
}

func UpdateBundledSkill(opts InstallOptions) (InstallReport, error) {
	return InstallBundledSkill(opts)
}

func UninstallBundledSkill(opts UninstallOptions) (UninstallReport, error) {
	if opts.AppID == "" {
		return emptyUninstallReport([]map[string]any{{"reason": "invalid-app-id"}}), nil
	}
	targets, errs, _, err := resolveInstallTargets(opts.BaseOptions, opts.Agents, opts.Scope, opts.SkillName, opts.AppID)
	if err != nil {
		return UninstallReport{}, err
	}
	report := emptyUninstallReport(errs)
	for _, target := range targets {
		result := targetResult(target)
		_, reason := inspectTarget(target.TargetDir, opts.AppID, opts.SkillName)
		switch reason {
		case "missing":
			report.Skipped = append(report.Skipped, withReason(result, "missing"))
		case "unmanaged", "owner-mismatch":
			report.Conflicts = append(report.Conflicts, withReason(result, reason))
		default:
			reason, err := removeManagedSkill(target.TargetDir, opts.AppID, opts.SkillName)
			if err != nil {
				return report, err
			}
			if reason != "" {
				report.Conflicts = append(report.Conflicts, withReason(result, reason))
				continue
			}
			report.Removed = append(report.Removed, result)
		}
	}
	return report, nil
}

func StatusBundledSkill(opts StatusOptions) (StatusReport, error) {
	if opts.AppID == "" {
		return emptyStatusReport([]map[string]any{{"reason": "invalid-app-id"}}), nil
	}
	targets, errs, _, err := resolveInstallTargets(opts.BaseOptions, opts.Agents, opts.Scope, opts.SkillName, opts.AppID)
	if err != nil {
		return StatusReport{}, err
	}
	report := emptyStatusReport(errs)
	for _, target := range targets {
		result := targetResult(target)
		meta, reason := inspectTarget(target.TargetDir, opts.AppID, opts.SkillName)
		switch reason {
		case "missing":
			report.Missing = append(report.Missing, result)
		case "unmanaged", "owner-mismatch":
			report.Conflicts = append(report.Conflicts, withReason(result, reason))
		default:
			report.Installed = append(report.Installed, InstalledTarget{TargetResult: result, Metadata: meta})
		}
	}
	return report, nil
}

func installOrPlan(opts InstallOptions, write bool) (InstallReport, error) {
	if opts.AppID == "" {
		return emptyInstallReport([]map[string]any{{"reason": "invalid-app-id"}}), nil
	}
	bundle, bundleMeta, err := resolveSkillBundle(opts.SkillBundle)
	if err != nil {
		reason := "invalid-skill-bundle"
		if opts.SkillBundle.kind == "github" {
			reason = "bundle-resolve-failed"
		}
		return emptyInstallReport([]map[string]any{{"reason": reason}}), nil
	}
	skill := validateNormalizedSkill(bundle)
	if !skill.Valid {
		return emptyInstallReport([]map[string]any{{"reason": skill.ErrorCode}}), nil
	}
	hash := contentHash(bundle)
	targets, errs, _, err := ResolveInstallTargets(opts.BaseOptions, opts.Agents, opts.Scope, skill.SkillName)
	if err != nil {
		return InstallReport{}, err
	}
	report := emptyInstallReport(errs)
	for _, target := range targets {
		result := targetResult(target)
		meta, reason := inspectTarget(target.TargetDir, opts.AppID, skill.SkillName)
		if reason != "" && reason != "missing" && !opts.Force {
			report.Conflicts = append(report.Conflicts, withReason(result, reason))
			continue
		}
		if reason == "" && meta.Hash == hash {
			repaired, repairErr := repairSkillBundleModes(bundle, target.TargetDir, write)
			if repairErr != nil {
				return report, repairErr
			}
			metadataChanged := bundleMeta.Explicit && !installedMetadataEqual(meta, newInstalledMetadata(opts.AppID, skill.SkillName, hash, bundleMeta))
			if !repaired && !metadataChanged {
				report.Skipped = append(report.Skipped, withReason(result, "unchanged"))
				continue
			}
			if write {
				err = writeMetadata(target.TargetDir, opts.AppID, skill.SkillName, hash, bundleMeta)
			}
		} else if write {
			err = writeManagedSkill(bundle, target.TargetDir, opts.AppID, skill.SkillName, hash, bundleMeta, reason != "missing")
		}
		if err != nil {
			return report, err
		}
		if reason == "missing" {
			report.Installed = append(report.Installed, result)
		} else {
			report.Updated = append(report.Updated, result)
		}
	}
	return report, nil
}

func targetResult(target TargetGroup) TargetResult {
	result := TargetResult{SkillName: target.SkillName, TargetDir: target.TargetDir}
	if len(target.HostIDs) == 1 {
		result.HostID = target.HostIDs[0]
	} else {
		result.HostIDs = target.HostIDs
	}
	return result
}

func withReason(result TargetResult, reason string) TargetStatus {
	return TargetStatus{TargetResult: result, Reason: reason}
}

func emptyInstallReport(errs []map[string]any) InstallReport {
	return InstallReport{
		Installed: []TargetResult{},
		Updated:   []TargetResult{},
		Skipped:   []TargetStatus{},
		Conflicts: []TargetStatus{},
		Errors:    reportErrors(errs),
	}
}

func emptyUninstallReport(errs []map[string]any) UninstallReport {
	return UninstallReport{
		Removed:   []TargetResult{},
		Skipped:   []TargetStatus{},
		Conflicts: []TargetStatus{},
		Errors:    reportErrors(errs),
	}
}

func emptyStatusReport(errs []map[string]any) StatusReport {
	return StatusReport{
		Installed: []InstalledTarget{},
		Missing:   []TargetResult{},
		Conflicts: []TargetStatus{},
		Errors:    reportErrors(errs),
	}
}

func reportErrors(errs []map[string]any) []ReportError {
	if errs == nil {
		return []ReportError{}
	}
	result := make([]ReportError, 0, len(errs))
	for _, err := range errs {
		result = append(result, ReportError{
			Agent:     stringField(err, "agent"),
			Flag:      stringField(err, "flag"),
			HostID:    stringField(err, "hostId"),
			Reason:    stringField(err, "reason"),
			Scope:     Scope(stringField(err, "scope")),
			SkillName: stringField(err, "skillName"),
			Value:     stringField(err, "value"),
		})
	}
	return result
}

func stringField(value map[string]any, key string) string {
	if text, ok := value[key].(string); ok {
		return text
	}
	return ""
}
