//go:build linux

package filesystems

import (
	"fmt"
	"time"

	"github.com/opensvc/om3/util/command"
	"github.com/rs/zerolog"
)

func (t T) Mount(dev string, mnt string, options string) error {
	args := []string{"-t", t.Type()}
	if len(options) > 0 {
		args = append(args, "-o", options)
	}
	args = append(args, dev, mnt)
	cmd := command.New(
		command.WithName("mount"),
		command.WithArgs(args),
		command.WithLogger(t.Log()),
		command.WithTimeout(time.Minute),
		command.WithCommandLogLevel(zerolog.InfoLevel),
		command.WithStdoutLogLevel(zerolog.InfoLevel),
		command.WithStderrLogLevel(zerolog.ErrorLevel),
	)
	cmd.Run()
	exitCode := cmd.ExitCode()
	if exitCode != 0 {
		return fmt.Errorf("%s exit code %d", cmd, exitCode)
	}
	return nil
}

func (t T) Umount(mnt string) error {
	cmd := command.New(
		command.WithName("umount"),
		command.WithVarArgs(mnt),
		command.WithLogger(t.Log()),
		command.WithTimeout(time.Minute),
		command.WithCommandLogLevel(zerolog.InfoLevel),
		command.WithStdoutLogLevel(zerolog.InfoLevel),
		command.WithStderrLogLevel(zerolog.ErrorLevel),
	)
	cmd.Run()
	exitCode := cmd.ExitCode()
	if exitCode != 0 {
		return fmt.Errorf("%s exit code %d", cmd, exitCode)
	}
	return nil
}
