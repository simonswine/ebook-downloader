package main

import (
	"log/slog"
	"os"

	"github.com/simonswine/ebook-downloader/derspiegel"
)

func init() {
	// Set the log level to debug
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))
	slog.SetDefault(logger)
}

var ()

func downloadSpiegel() error {
	b := derspiegel.New(
		os.Getenv("DER_SPIEGEL_USERNAME"),
		os.Getenv("DER_SPIEGEL_PASSWORD"),
	)

	f, err := os.Create("spiegel_latest.pdf")
	if err != nil {
		return err
	}

	err = b.DownloadLatest(f)
	if err != nil {
		return err
	}

	return nil
}

func checkError(err error, msg string, args ...any) {
	if err == nil {
		return
	}
	args = append([]any{"err", err}, args...)
	slog.Error(msg, args...)
	os.Exit(1)
}

func main() {
	checkError(downloadSpiegel(), "Failed to download spiegel")
}
