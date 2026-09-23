package syslet

import (
	"strings"
	"tool/cli"
	"tool/exec"
)

// Usage: cue cmd plan
command: plan: {
	runDeploy: exec.Run & {
		cmd: ["ssh", fqdn, "syslet", "--stdin", "--diff"]
		stdin:  syslet.specRendered
		stdout: string
	}
	show: cli.Print & {
		text: runDeploy.stdout
	}
}

// Usage: cue cmd apply
command: apply: {
	runDeploy: exec.Run & {
		cmd: ["ssh", fqdn, "syslet", "--stdin", "--diff"]
		stdin:  syslet.specRendered
		stdout: string
	}
	showDiff: cli.Print & {
		text: runDeploy.stdout
	}
	maybeContinue: {
		if !strings.Contains(runDeploy.stdout, "No changes detected.") {
			askConfirm: cli.Ask & {
				$after:   showDiff
				prompt:   "Continue? (yes/no)"
				response: string
			}
			continue: {
				if askConfirm.response == "yes" {
					runApply: exec.Run & {
						cmd: ["ssh", fqdn, "syslet", "--stdin"]
						stdin:  syslet.specRendered
						stdout: string
					}

				}
			}
		}
	}
}
