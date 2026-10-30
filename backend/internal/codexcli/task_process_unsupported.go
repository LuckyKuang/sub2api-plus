//go:build !darwin && !linux

package codexcli

import (
	"errors"
	"os/exec"
)

func configureTaskProcess(*exec.Cmd) error {
	return errors.New("CLI task process groups are unsupported on this platform")
}
