package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/simonswine/ebook-downloader/calibredb"
	"github.com/urfave/cli/v3"
)

func init() {
	// Set the log level to debug
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))
	slog.SetDefault(logger)
}

func checkError(err error, msg string, args ...any) {
	if err == nil {
		return
	}
	args = append([]any{"err", err}, args...)
	slog.Error(msg, args...)
	os.Exit(1)
}

func newDB(cmd *cli.Command) *calibredb.CalibreDB {
	return calibredb.New(cmd.String("calibredb-path"))
}

func run(ctx context.Context, args []string) error {
	cmd := &cli.Command{
		Name: "ebook-downloader",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "show debug logs",
			},
			&cli.StringFlag{
				Name:  "calibredb-path",
				Usage: "path to calibre database",
				Value: "/var/lib/media/Books/",
			},
		},
		EnableShellCompletion: true,
		Commands: []*cli.Command{
			donaukurierCmd,
			derSpiegelCmd,
		},
	}

	return cmd.Run(ctx, args)
}

func main() {
	ctx := context.Background()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	checkError(run(ctx, os.Args), "fatal error occurred")
}
