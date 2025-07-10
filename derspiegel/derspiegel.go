package derspiegel

import (
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/headzoo/surf/browser"
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

type issue struct {
	title string
	year  int64
	issue int64
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

func (b *DerSpiegel) downloadPDF(i *issue) error {
	uStart, err := url.Parse(PDF_DOWNLOAD_START_URL)
	if err != nil {
		return fmt.Errorf("failed to parse PDF download URL: %w", err)
	}
	uDownload, err := url.Parse(PDF_DOWNLOAD_DOWNLOAD_URL)
	if err != nil {
		return fmt.Errorf("failed to parse PDF download URL: %w", err)
	}

	q := uStart.Query()
	q.Set("heft", fmt.Sprintf("SP/%d/%d", i.year, i.issue))
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

	fpath := fmt.Sprintf("derspiegel-%04d-%02d.pdf", i.year, i.issue)
	slog.Info("Downloading PDF", "title", brow.Title(), "path", fpath)

	f, err := os.Create(fpath)
	if err != nil {
		return fmt.Errorf("failed to create PDF file: %w", err)
	}
	defer f.Close()

	_, err = brow.Download(f)
	if err != nil {
		return fmt.Errorf("failed to download PDF: %w", err)
	}

	return nil
}

func (d *DerSpiegel) browser() *browser.Browser {
	return surf.NewBrowser()
}

func (d *DerSpiegel) listIssues(year int) ([]issue, error) {
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
	issues := make([]issue, 0, 52)
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
		issues = append(issues, issue{
			title: title,
			year:  year,
			issue: issueNo,
		})
	})

	return issues, err
}

func (d *DerSpiegel) DownloadLatest(f io.Writer) error {
	slog.Info("Downloading latest Der Spiegel issue...")

	list, err := d.listIssues(0)
	if err != nil {
		return fmt.Errorf("failed to list issues: %w", err)
	}

	if len(list) == 0 {
		return fmt.Errorf("no issues found")
	}

	i := list[len(list)-1]
	slog.Info("Latest issue found", "title", i.title, "year", i.year, "issue", i.issue)

	err = d.downloadPDF(&i)
	if err != nil {
		return err
	}

	return nil

}
