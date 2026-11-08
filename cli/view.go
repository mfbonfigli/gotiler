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

func viewerFlags(port *int, ionToken *string) []cli.Flag {
	return []cli.Flag{
		&cli.IntFlag{
			Name:        "port",
			Usage:       "local port of the viewer. 0 (default) picks a free port.",
			Destination: port,
		},
		&cli.StringFlag{
			Name:        "ion-token",
			Usage:       "Cesium ion access token: the viewer then shows Cesium World Imagery and World Terrain instead of the default satellite imagery. Also read from the CESIUM_ION_TOKEN environment variable.",
			Sources:     cli.EnvVars("CESIUM_ION_TOKEN"),
			Destination: ionToken,
		},
	}
}

func newViewCommand() *cli.Command {
	var opts viewer.Options
	return &cli.Command{
		Name:      "view",
		Usage:     "show a tileset on a 3D globe in the browser and move it into place",
		ArgsUsage: "<tileset.json or the folder holding it>",
		Flags:     viewerFlags(&opts.Port, &opts.IonToken),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				_ = cli.ShowSubcommandHelp(cmd)
				return errors.New("the path of one tileset.json, or of the folder holding it, must be set")
			}
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()
			return serveViewer(ctx, cmd.Args().First(), opts)
		},
	}
}
