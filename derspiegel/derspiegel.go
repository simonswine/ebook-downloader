package derspiegel

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/headzoo/surf/browser"
	"github.com/simonswine/ebook-downloader/meta"
	"gopkg.in/headzoo/surf.v1"
)

const (
	MAGAZINES_URL             = "https://www.spiegel.de/spiegel/print"
	PDF_DOWNLOAD_START_URL    = "https://gruppenkonto.spiegel.de/download/start.html"
	PDF_DOWNLOAD_DOWNLOAD_URL = "https://gruppenkonto.spiegel.de/download/download.html"
)

type DerSpiegel struct {
	username, password string
}

func New(username, password string) *DerSpiegel {
	return &DerSpiegel{
		username: username,
		password: password,
	}
}

type Issue struct {
	Title string
	Year  int
	Issue int
}

func (b *DerSpiegel) login(brow *browser.Browser) error {
	slog.Debug("Logging into spiegel")
	form, err := brow.Form("form[name=loginform]")
	if err != nil {
		return fmt.Errorf("failed to find login form: %w", err)
	}

	if err := form.Input("loginform:username", b.username); err != nil {
		return fmt.Errorf("failed to set username: %w", err)
	}

	if err := form.Submit(); err != nil {
		return fmt.Errorf("failed to submit form: %w", err)
	}

	if brow.Title() != "Anmelden" {
		return fmt.Errorf("unexpected title, most likely wrong username: %s", brow.Title())
	}

	form, err = brow.Form("form[name=loginform]")
	if err != nil {
		return fmt.Errorf("failed to find login form: %w", err)
	}

	if err := form.Input("loginform:password", b.password); err != nil {
		return fmt.Errorf("failed to set password: %w", err)
	}

	if err := form.Submit(); err != nil {
		return fmt.Errorf("failed to submit form: %w", err)
	}

	if brow.Title() == "Anmelden" {
		return fmt.Errorf("unexpected title, most likely wrong password: %s", brow.Title())
	}
	return nil
}

func (b *DerSpiegel) Download(i *meta.Info, fPDF *os.File) error {
	uStart, err := url.Parse(PDF_DOWNLOAD_START_URL)
	if err != nil {
		return fmt.Errorf("failed to parse PDF download URL: %w", err)
	}
	uDownload, err := url.Parse(PDF_DOWNLOAD_DOWNLOAD_URL)
	if err != nil {
		return fmt.Errorf("failed to parse PDF download URL: %w", err)
	}

	q := uStart.Query()
	q.Set("heft", fmt.Sprintf("SP/%d/%d", *i.Year, *i.Issue))
	uStart.RawQuery = q.Encode()
	uDownload.RawQuery = q.Encode()

	brow := b.browser()
	err = brow.Open(uStart.String())
	if err != nil {
		return err
	}

	if brow.Title() == "Anmelden" {
		if err := b.login(brow); err != nil {
			return err
		}
	}

	if !strings.Contains(brow.Title(), "Magazine") {
		return fmt.Errorf("unexpected title after login: %s", brow.Title())
	}

	err = brow.Open(uDownload.String())
	if err != nil {
		return err
	}

	fPDFTemp, err := os.CreateTemp("", "der-spiegel-*.pdf")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	//	defer os.Remove(fPDFTemp.Name())

	_, err = brow.Download(fPDFTemp)
	if err != nil {
		return fmt.Errorf("failed to download PDF: %w", err)
	}
	if err := fPDFTemp.Close(); err != nil {
		return fmt.Errorf("error closing temp file: %w", err)
	}
	slog.Debug("downloaded pdf", "temp_path", fPDFTemp.Name())

	if err := b.updateBookmarks(i, fPDFTemp.Name(), fPDF); err != nil {
		return fmt.Errorf("failed to update bookmarks: %w", err)
	}

	if err := fPDF.Close(); err != nil {
		return fmt.Errorf("error closing pdf file: %w", err)
	}

	if err := meta.WriteEbookMeta(fPDF.Name(), i); err != nil {
		return fmt.Errorf("failed to write ebook meta: %w", err)
	}

	return nil
}

func (d *DerSpiegel) browser() *browser.Browser {
	return surf.NewBrowser()
}

func (d *DerSpiegel) ListIssues(year int) ([]*meta.Info, error) {
	bow := d.browser()
	u := MAGAZINES_URL
	if year > 0 {
		u += fmt.Sprintf("/index-%04d.html", year)
	}
	err := bow.Open(u)
	if err != nil {
		return nil, err
	}

	// find all issues
	issues := make([]*meta.Info, 0, 52)
	bow.Find("div.h-full>a").Each(func(_ int, s *goquery.Selection) {
		link, exists := s.Attr("href")
		if !exists {
			return
		}

		// skip audio links
		if strings.Contains(link, "/audio/") {
			return
		}

		// get year and month from the link
		split := strings.Split(link, "/")
		split = strings.Split(split[len(split)-1], "-")

		if len(split) < 3 {
			slog.Debug("Skipping link, no year, issue found", "link", link)
			return
		}
		year, err := strconv.ParseInt(split[1], 10, 64)
		if err != nil {
			slog.Error("Failed to parse year", "error", err, "link", link, "year", split[1])
			return
		}

		issueStr := split[2]
		if pos := strings.Index(issueStr, "."); pos != -1 {
			issueStr = issueStr[:pos]
		}
		issueNo, err := strconv.ParseInt(issueStr, 10, 64)
		if err != nil {
			slog.Error("Failed to parse issue number", "error", err, "link", link, "issue", issueStr)
			return
		}

		title, exists := s.Attr("title")
		if !exists {
			return
		}
		slog.Debug("Found issue", "title", title, "link", link, "year", year, "issue", issueNo)

		yearInt := int(year)
		issueInt := int(issueNo)
		issues = append(issues, &meta.Info{
			Author:   "SPIEGEL-Verlag, Hamburg",
			Title:    "DER SPIEGEL",
			Subtitle: &title,
			Year:     &yearInt,
			Issue:    &issueInt,
			Language: "de",
			Category: meta.CategoryMagazine,
		})
	})

	return issues, err
}

func (d *DerSpiegel) updateBookmarks(info *meta.Info, path string, w io.Writer) error {
	bufOut := bytes.NewBuffer(nil)
	if err := meta.ExtractText(path, 4, 5, bufOut); err != nil {
		return fmt.Errorf("failed to extract text: %w", err)
	}

	// get bookmarks
	bookmarks, err := parseTOC(bufOut)
	if err != nil {
		return fmt.Errorf("failed to parse TOC: %w", err)
	}

	// add static bookmarks
	bookmarks.Bookmarks = append([]meta.Bookmark{
		{
			Title:      "TITELSEITE",
			PageNumber: 1,
			Level:      1,
		},
		{
			Title:      "HAUSMITTEILUNG",
			PageNumber: 3,
			Level:      1,
		},
		{
			Title:      "INHALT",
			PageNumber: 4,
			Level:      1,
		},
	}, bookmarks.Bookmarks...)

	info.PublishingDate = bookmarks.PublishingDate
	return meta.ReplaceBookmarks(w, path, bookmarks.Bookmarks)
}

// ShouldDownloadIssue determines if an issue should be downloaded based on the lastBook in the database
// This implements the nil-safe logic to avoid panics
func ShouldDownloadIssue(lastBook *meta.Info, issue *meta.Info) bool {
	// If there's no lastBook or it has no issue number, download everything
	if lastBook == nil || lastBook.Issue == nil {
		return true
	}

	// If the issue has no number, download it (edge case)
	if issue.Issue == nil {
		return true
	}

	// Download if this issue is newer than the last book
	return *issue.Issue > *lastBook.Issue
}
