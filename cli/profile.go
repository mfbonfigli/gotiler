package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
)

// profiler profiles whichever command runs, as the global --profile and
// --profile-output flags ask. The root command starts it before the command
// and stops it after; commands that exit early stop it themselves, with
// finish.
type profiler struct {
	kind   string
	output string
	stop   func()
}

// flags returns the global profiling flags, which every command takes, on
// either side of the command name. They are for developers, so the help does
// not list them.
func (p *profiler) flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:        "profile",
			Usage:       "profile the command with pprof: 'cpu' for a CPU profile, 'memory' for a heap profile taken when the command ends. Works with every command.",
			Destination: &p.kind,
			Hidden:      true,
		},
		&cli.StringFlag{
			Name:        "profile-output",
			Usage:       "the file the profile is written to. Defaults to 'pprof_<type>.prof' in the current folder.",
			Destination: &p.output,
			Hidden:      true,
		},
	}
}

// start starts the requested profile, as the root command's Before hook.
func (p *profiler) start(ctx context.Context, _ *cli.Command) (context.Context, error) {
	switch strings.ToLower(p.kind) {
	case "":
		return ctx, nil
	case "cpu", "memory":
		p.stop = SetupProfiling(p.kind, p.output)
		return ctx, nil
	default:
		return ctx, fmt.Errorf("--profile must be 'cpu' or 'memory', not %q", p.kind)
	}
}

// after stops the profile, as the root command's After hook.
func (p *profiler) after(context.Context, *cli.Command) error {
	p.finish()
	return nil
}

// finish stops the profile and writes it, the first time it is called.
func (p *profiler) finish() {
	if p == nil || p.stop == nil {
		return
	}
	stop := p.stop
	p.stop = nil
	stop()
}
