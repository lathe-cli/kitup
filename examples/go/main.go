package main

import (
	"encoding/json"
	"os"

	kitup "github.com/samzong/kitup/go"
)

func main() {
	report, err := kitup.InstallBundledSkill(kitup.InstallOptions{
		AppID:       "kitup-example-go",
		SkillBundle: kitup.DirectoryBundle("../../skills/kitup"),
		Scope:       kitup.UserScope,
	})
	if err != nil {
		panic(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		panic(err)
	}
	if len(report.Errors)+len(report.Conflicts) > 0 {
		os.Exit(1)
	}
}
