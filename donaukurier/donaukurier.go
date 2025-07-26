package donaukurier

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/headzoo/surf"
	"github.com/headzoo/surf/browser"
	"github.com/simonswine/ebook-downloader/meta"
)

const (
	MAGAZINES_LIST_URL = "https://epaper.donaukurier.de/epaper/%d/"
	LOGIN_URL          = "https://epaper.donaukurier.de/anmelden"
	PAGE_METADATA_URL  = "https://epaper.donaukurier.de/api/v1/media/%s.json"
	DOWNLOAD_URL       = "https://epaper.donaukurier.de/download/%s"

	HILPOLTSTEINER_KURIER = 2622

	AnnotationNewspaperID = "donaukurier.newspaper_id"
)

type Donaukurier struct {
	username, password string

	browserPublic   *browser.Browser
	browserLoggedIn *browser.Browser
	browserMtx      sync.Mutex
}

func New(username, password string) *Donaukurier {
	return &Donaukurier{
		username: username,
		password: password,
	}
}

func (d *Donaukurier) browser() *browser.Browser {
	d.browserMtx.Lock()
	defer d.browserMtx.Unlock()

	if d.browserPublic != nil {
		return d.browserPublic
	}

	d.browserPublic = surf.NewBrowser()
	return d.browserPublic
}

func (d *Donaukurier) login() (*browser.Browser, error) {
	d.browserMtx.Lock()
	defer d.browserMtx.Unlock()

	if d.browserLoggedIn != nil {
		return d.browserLoggedIn, nil
	}

	brow := surf.NewBrowser()
	slog.Debug("Login at donaukurier", "username", d.username)
	err := brow.Open(LOGIN_URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open login page: %w", err)
	}

	form, err := brow.Form("form.login")
	if err != nil {
		return nil, fmt.Errorf("failed to find login form: %w", err)
	}

	if err := form.Input("username", d.username); err != nil {
		return nil, fmt.Errorf("failed to set username: %w", err)
	}

	if err := form.Input("password", d.password); err != nil {
		return nil, fmt.Errorf("failed to set password: %w", err)
	}

	if err := form.Submit(); err != nil {
		return nil, fmt.Errorf("failed to submit login form: %w", err)
	}

	if brow.Title() == "Account" {
		return nil, fmt.Errorf("login failed")
	}

	slog.Debug("Login at donaukurier successful", "username", d.username)

	d.browserLoggedIn = brow
	return d.browserLoggedIn, nil
}

func (d *Donaukurier) Download(info *meta.Info, fPDF *os.File) (err error) {
	brow, err := d.login()
	if err != nil {
		return err
	}

	newspaperID := info.Annotations[AnnotationNewspaperID].(int)
	if newspaperID == 0 {
		return fmt.Errorf("newspaper id not given")
	}

	u := fmt.Sprintf(MAGAZINES_LIST_URL, newspaperID) + time.Time(info.PublishingDate).Format("02.01.2006")
	slog.Debug("Opening issue page", "url", u)
	if err := brow.Open(u); err != nil {
		return err
	}

	// get URL to the web view section
	webviewLink := brow.Find("a.newspaper__link").First()
	if webviewLink == nil {
		return fmt.Errorf("no webview link found")
	}

	webviewURL, exists := webviewLink.Attr("href")
	if !exists {
		return fmt.Errorf("no webview link href attribute found")
	}

	urlParts := strings.Split(webviewURL, "/")
	issueID := urlParts[len(urlParts)-1]
	slog.Debug("Got newspaper issue id", "issueID", issueID)

	fMeta, err := os.CreateTemp("", "donaukurier-*.json")
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, os.Remove(fMeta.Name()))
	}()

	uMeta := fmt.Sprintf(PAGE_METADATA_URL, issueID)
	err = brow.Open(uMeta)
	if err != nil {
		return err
	}

	if _, err := brow.Download(fMeta); err != nil {
		return err
	}

	fPDFTemp, err := os.CreateTemp("", "donaukurier-*.pdf")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		err = errors.Join(err, os.Remove(fPDFTemp.Name()))
	}()

	uPDF := fmt.Sprintf(DOWNLOAD_URL, issueID)
	err = brow.Open(uPDF)
	if err != nil {
		return err
	}

	if _, err := brow.Download(fPDFTemp); err != nil {
		return fmt.Errorf("failed to download PDF: %w", err)
	}

	slog.Debug("Downloaded PDF", "file", fPDFTemp.Name())
	slog.Debug("Downloaded meta", "file", fMeta.Name())

	if _, err := fMeta.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to start of PDF: %w", err)
	}

	bookmarks, err := parseToc(fMeta)
	if err != nil {
		return fmt.Errorf("failed to parse bookmarks: %w", err)
	}

	// add static bookmarks
	bookmarks = append([]meta.Bookmark{
		{
			Title:      "Titelseite",
			PageNumber: 1,
			Level:      1,
		},
	}, bookmarks...)

	if _, err := fPDFTemp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to start of PDF: %w", err)
	}

	if err := meta.ReplaceBookmarks(fPDF, fPDFTemp.Name(), bookmarks); err != nil {
		return fmt.Errorf("failed to replace bookmarks: %w", err)
	}

	if err := fPDF.Close(); err != nil {
		return fmt.Errorf("failed to close PDF: %w", err)
	}

	if err := d.detectIssue(info, fPDF.Name()); err != nil {
		return fmt.Errorf("failed to detect issue: %w", err)
	}

	if err := meta.WriteEbookMeta(fPDF.Name(), info); err != nil {
		return fmt.Errorf("failed to write ebook meta: %w", err)
	}

	return nil
}

func (d *Donaukurier) detectIssue(info *meta.Info, path string) error {
	frontText := bytes.NewBuffer(nil)
	if err := meta.ExtractText(path, 1, 1, frontText); err != nil {
		return fmt.Errorf("failed to extract text: %w", err)
	}

	// Iterate line by line and find the first line that contains the title
	scanner := bufio.NewScanner(frontText)
	needle := "Nr."
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, needle) {
			idx := strings.Index(line, ",")
			if idx == -1 {
				continue
			}

			issue, err := strconv.Atoi(line[4:idx])
			if err != nil {
				return fmt.Errorf("failed to convert issue to int: %w", err)
			}
			info.Issue = &issue
			slog.Debug("Found issue number in year", "issue", issue)

			break
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to scan front text: %w", err)
	}
	if info.Issue == nil {
		return fmt.Errorf("issue not found")
	}

	return nil
}

func (d *Donaukurier) ListIssues(newspaperID int) ([]*meta.Info, error) {
	bow := d.browser()
	u := fmt.Sprintf(MAGAZINES_LIST_URL, newspaperID)
	err := bow.Open(u)
	if err != nil {
		return nil, err
	}

	title := ""
	switch newspaperID {
	case HILPOLTSTEINER_KURIER:
		title = "Hilpoltsteiner Kurier"
	default:
		return nil, fmt.Errorf("unknown newspaper id: %d", newspaperID)
	}

	// find all issues
	var issues []*meta.Info
	bow.Find("div.dd__list__datepicker").Each(func(_ int, s *goquery.Selection) {
		// already found issues, stop
		if len(issues) > 0 {
			return
		}

		releases, exists := s.Attr("data-releases")
		if !exists {
			return
		}

		for _, dateStr := range strings.Split(releases, ";") {
			if dateStr == "" {
				continue
			}

			date, err := time.Parse(meta.DATE_FORMAT, dateStr)
			if err != nil {
				slog.Error("Failed to parse date", "error", err, "date", dateStr)
				continue
			}

			year := date.Year()
			issues = append(issues, &meta.Info{
				PublishingDate: meta.PublishingDate(date),
				Author:         "Donaukurier, GmbH",
				Title:          title,
				Year:           &year,
				Language:       "de",
				Category:       meta.CategoryNewspaper,
				Annotations: map[string]any{
					AnnotationNewspaperID: newspaperID,
				},
			})
		}
	})

	return issues, err
}
