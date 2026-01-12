package collector

import (
	"context"
	"os/exec"
)

func hasBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
