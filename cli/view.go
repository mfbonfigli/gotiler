package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/mfbonfigli/gotiler/v3/internal/viewer"
	"github.com/urfave/cli/v3"
)

// serveViewer shows a tileset in the browser viewer, opening it in the default
// browser, and blocks until the viewer stops. Tests replace it to avoid
// starting a server.
var serveViewer = func(ctx context.Context, tilesetPath string, opts viewer.Options) error {
	server, err := viewer.NewServer(tilesetPath, opts)
	if err != nil {
		return err
	}
	url := server.URL()
	fmt.Fprintf(os.Stderr, "\nGoTiler viewer: %s\nThe viewer stops when its page is closed, or with Ctrl+C.\n", url)
	if err := openInBrowser(url); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open the browser (%v): open the address above.\n", err)
	}
	if err := server.Serve(ctx); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Viewer stopped.")
	return nil
}

// openInBrowser opens url in the default browser. Tests replace it to avoid
// launching one.
var openInBrowser = func(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// ionTokenEnvVar names the environment variable holding the viewer's Cesium
// ion access token. There is no flag for it, so the token does not end up in
// shell histories or process listings.
const ionTokenEnvVar = "CESIUM_ION_TOKEN"

func viewerPortFlag(port *int) cli.Flag {
	return &cli.IntFlag{
		Name:        "port",
		Usage:       "local port of the viewer. 0 (default) picks a free port.",
		Destination: port,
	}
}

func newViewCommand() *cli.Command {
	var opts viewer.Options
	return &cli.Command{
		Name:        "view",
		Usage:       "show a tileset on a 3D globe in the browser and move it into place",
		Description: "Set the " + ionTokenEnvVar + " environment variable to a Cesium ion access token to show Cesium World Imagery and World Terrain instead of the default satellite imagery.",
		ArgsUsage:   "<tileset.json or the folder holding it>",
		Flags:       []cli.Flag{viewerPortFlag(&opts.Port)},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				_ = cli.ShowSubcommandHelp(cmd)
				return errors.New("the path of one tileset.json, or of the folder holding it, must be set")
			}
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()
			opts.IonToken = os.Getenv(ionTokenEnvVar)
			return serveViewer(ctx, cmd.Args().First(), opts)
		},
	}
}
