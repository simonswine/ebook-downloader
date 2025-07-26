package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/simonswine/ebook-downloader/calibredb"
	"github.com/simonswine/ebook-downloader/derspiegel"
	"github.com/simonswine/ebook-downloader/meta"
	"github.com/urfave/cli/v3"
)

func newDerSpiegel(cmd *cli.Command) *derspiegel.DerSpiegel {
	return derspiegel.New(cmd.String("username"), cmd.String("password"))
}

func downloadDerSpiegel(d *derspiegel.DerSpiegel, issue *meta.Info, db *calibredb.CalibreDB) error {
	f, err := os.Create(fmt.Sprintf("der-spiegel-%04d-%04d.pdf", *issue.Year, *issue.Issue))
	if err != nil {
		return err
	}

	slog.Info("Downloading issue", "year", *issue.Year, "issue", *issue.Issue, "path", f.Name())
	if err := d.Download(issue, f); err != nil {
		return err
	}

	if db != nil {
		if err := db.AddBook(f.Name()); err != nil {
			return err
		}
	}

	return nil
}

var derSpiegelCmd = &cli.Command{
	Name: "der-spiegel",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "username",
			Required: true,
			Sources: cli.ValueSourceChain{
				Chain: []cli.ValueSource{
					cli.EnvVar("DER_SPIEGEL_USERNAME"),
				},
			},
		},
		&cli.StringFlag{
			Name:     "password",
			Required: true,
			Sources: cli.ValueSourceChain{
				Chain: []cli.ValueSource{
					cli.EnvVar("DER_SPIEGEL_PASSWORD"),
				},
			},
		},
	},
	Commands: []*cli.Command{
		{
			Name:  "list",
			Usage: "list issues from DER SPIEGEL",
			Action: func(ctx context.Context, cmd *cli.Command) error {
				d := newDerSpiegel(cmd)

				issues, err := d.ListIssues(0)
				if err != nil {
					return err
				}

				slog.Info("Found issues", "count", len(issues))
				for _, issue := range issues {
					slog.Info("Issue", "year", *issue.Year, "issue", *issue.Issue, "title", *issue.Subtitle)
				}

				return nil
			},
		},
		{
			Name:  "download",
			Usage: "download issues from donaukurier",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:  "max-issues",
					Value: 31,
					Usage: "maximum number of issues to download, 0 for all",
				},
				&cli.BoolFlag{
					Name:  "add-to-calibredb",
					Value: false,
					Usage: "add downloaded issues to calibredb",
				},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				d := newDerSpiegel(cmd)

				issues, err := d.ListIssues(0)
				if err != nil {
					return err
				}

				var db *calibredb.CalibreDB
				if cmd.Bool("add-to-calibredb") {
					db = newDB(cmd)
				}

				count := 0
				maxIssues := cmd.Int("max-issues")
				for _, issue := range issues {

					if err := downloadDerSpiegel(d, issue, db); err != nil {
						return err
					}

					count++
					if count >= maxIssues && maxIssues != 0 {
						break
					}
				}

				return nil
			},
		},
		{
			Name:  "sync",
			Usage: "download issues from donaukurier, which are not in calibredb",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:  "max-issues",
					Value: 31,
					Usage: "maximum number of issues to download, 0 for all",
				},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				d := newDerSpiegel(cmd)
				issues, err := d.ListIssues(0)
				if err != nil {
					return err
				}

				if len(issues) == 0 {
					return errors.New("no issues found")
				}

				db := newDB(cmd)
				lastBook, err := db.LastBook(issues[0].Series())
				if err != nil {
					return err
				}
				if lastBook.Issue == nil {
					return errors.New("no issue number detected in last book")
				}

				count := 0
				maxIssues := cmd.Int("max-issues")
				for _, issue := range issues {
					if issue.Issue == nil || *lastBook.Issue >= *issue.Issue {
						break
					}

					if err := downloadDerSpiegel(d, issue, db); err != nil {
						return err
					}

					count++
					if count >= maxIssues && maxIssues != 0 {
						break
					}
				}

				return nil
			},
		},
	},
}
