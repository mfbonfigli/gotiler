package main

import (
	"context"
	"fmt"
	"os"

	gotilercli "github.com/mfbonfigli/gotiler/v3/cli"
)

// Version is injected dynamically at build time via `go build -ldflags "-X main.Version=X.Y.Z"`.
// When not set at build time, the CLI package default is used as fallback.
var Version string = ""

// GitCommit is injected dynamically at build time via `go build -ldflags "-X main.GitCommit=XYZ"`.
var GitCommit string = "(na)"

func main() {
	build := gotilercli.BuildInfo{
		Version:        Version,
		DefaultVersion: gotilercli.DefaultVersion,
		GitCommit:      GitCommit,
	}
	branding := gotilercli.DefaultBranding()
	gotilercli.PrintBanner(os.Stderr, branding, build)
	app := gotilercli.NewApp(gotilercli.Options{
		BuildInfo:     build,
		Branding:      branding,
		ExtraCommands: []gotilercli.CommandFactory{},
	})
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
