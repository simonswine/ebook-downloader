package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/simonswine/ebook-downloader/calibredb"
	"github.com/simonswine/ebook-downloader/donaukurier"
	"github.com/simonswine/ebook-downloader/meta"
)

func newDonaukurier(cmd *cli.Command) *donaukurier.Donaukurier {
	return donaukurier.New(cmd.String("username"), cmd.String("password"))
}

func downloadDonaukurier(d *donaukurier.Donaukurier, issue *meta.Info, db *calibredb.CalibreDB) error {
	f, err := os.Create(fmt.Sprintf("donaukurier-%s.pdf", issue.PublishingDate.Format("2006-01-02")))
	if err != nil {
		return err
	}

	slog.Info("Downloading issue", "date", issue.PublishingDate, "path", f.Name())
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

var donaukurierCmd = &cli.Command{
	Name: "donaukurier",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "username",
			Required: true,
			Sources: cli.ValueSourceChain{
				Chain: []cli.ValueSource{
					cli.EnvVar("DONAUKURIER_USERNAME"),
				},
			},
		},
		&cli.StringFlag{
			Name:     "password",
			Required: true,
			Sources: cli.ValueSourceChain{
				Chain: []cli.ValueSource{
					cli.EnvVar("DONAUKURIER_PASSWORD"),
				},
			},
		},
	},
	Commands: []*cli.Command{
		{
			Name:  "list",
			Usage: "list issues from donaukurier",
			Action: func(ctx context.Context, cmd *cli.Command) error {
				d := newDonaukurier(cmd)

				issues, err := d.ListIssues(donaukurier.HILPOLTSTEINER_KURIER)
				if err != nil {
					return err
				}

				slog.Info("Found issues", "count", len(issues))
				for _, issue := range issues {
					slog.Info("Issue", "date", issue.PublishingDate, "title", issue.Title)
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
				d := newDonaukurier(cmd)

				issues, err := d.ListIssues(donaukurier.HILPOLTSTEINER_KURIER)
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

					if err := downloadDonaukurier(d, issue, db); err != nil {
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
				d := newDonaukurier(cmd)
				issues, err := d.ListIssues(donaukurier.HILPOLTSTEINER_KURIER)
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

				count := 0
				maxIssues := cmd.Int("max-issues")
				for _, issue := range issues {
					if issue.PublishingDate.Time().Compare(lastBook.PublishingDate.Time()) <= 0 {
						continue
					}

					if err := downloadDonaukurier(d, issue, db); err != nil {
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
