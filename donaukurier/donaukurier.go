package donaukurier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/simonswine/ebook-downloader/meta"
)

const (
	apiBase    = "https://mgb-dk-api.prod.twipecloud.net"
	apiKey     = "mgb-dk-tenant-identifier"
	epaperBase = "https://epaper.donaukurier.de"

	appName              = "webApp"
	subscriptionProvider = "TWIPE-DEMO" // mgb-dk does not override the default
	clientVersion        = "1.0.0"
	platformVersion      = "3.41.8"

	HILPOLTSTEINER_KURIER = "MGBDKHIP"

	AnnotationContentPackageID = "donaukurier.content_package_id"

	maxAuthRetries = 4
	retryBase      = 3 * time.Second
)

type Donaukurier struct {
	username, password string

	mu             sync.Mutex
	httpClient     *http.Client
	deviceUUID     string

	// authenticated session state (protected by mu)
	userID         int
	subscriptionID int
	sessionID      int
	sessionExpires time.Time
}

func New(username, password string) *Donaukurier {
	return &Donaukurier{
		username:   username,
		password:   password,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		deviceUUID: uuid.New().String(),
	}
}

// --- HTTP helpers ---

func (d *Donaukurier) apiDo(method, path string, body, out interface{}) error {
	u := apiBase + path
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: status %d: %s", method, u, resp.StatusCode, string(b))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (d *Donaukurier) apiGet(path string, out interface{}) error {
	return d.apiDo("GET", path, nil, out)
}

func (d *Donaukurier) apiPost(path string, body, out interface{}) error {
	return d.apiDo("POST", path, body, out)
}

// --- Auth ---

type openSessionResponse struct {
	SessionInfo struct {
		SessionId int `json:"SessionId"`
		UserId    int `json:"UserId"`
	} `json:"SessionInfo"`
	ReturnCode *string `json:"ReturnCode"`
}

type validateSubscriptionResponse struct {
	Status         string `json:"Status"`
	StatusMessage  string `json:"StatusMessage"`
	UserID         int    `json:"UserID"`
	SubscriptionID int    `json:"SubscriptionID"`
}

func (d *Donaukurier) openSession(userID int) (openSessionResponse, error) {
	path := fmt.Sprintf("/Session/SessionService.svc/json/OpenSession/%s/%d/%s/%s/%s",
		appName, userID, d.deviceUUID, clientVersion, clientVersion)
	var resp openSessionResponse
	if err := d.apiGet(path, &resp); err != nil {
		return resp, fmt.Errorf("OpenSession(%d): %w", userID, err)
	}
	return resp, nil
}

// isTransient reports whether the Validate_Subscription status message indicates
// a temporary backend-unavailability condition (E1/E2 = "Systeme nicht verfügbar")
// that should be retried, as opposed to a credential/subscription error.
func isTransient(msg string) bool {
	switch msg {
	case "E1", "E2", "E3", "E9", "E13", "E15", "E17", "E18", "E20", "E21":
		return true
	}
	return false
}

// authHint returns a human-readable description for known status codes.
func authHint(msg string) string {
	switch msg {
	case "E5":
		return "no valid subscription"
	case "E6", "E7", "E26":
		return "invalid credentials (check email/password)"
	case "E1", "E2", "E3", "E9", "E13", "E15", "E17", "E18", "E20", "E21":
		return "systems unavailable"
	}
	return "unknown error"
}

func isSuccess(status string) bool {
	// The API returns "Succes" (sic) on success.
	return status == "Succes" || status == "Success" || status == "success"
}

// login performs the 3-step username/password login flow:
//  1. OpenSession(0, UUID)   → anonymous sessionId/userId
//  2. Validate_Subscription  → real userID + subscriptionID
//  3. OpenSession(userID, UUID) → authenticated sessionId
//
// Transient E1/E2-class errors are retried with linear backoff.
func (d *Donaukurier) login() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.sessionID != 0 && time.Now().Before(d.sessionExpires) {
		return nil
	}

	var lastErr error
	for attempt := 1; attempt <= maxAuthRetries; attempt++ {
		if attempt > 1 {
			wait := retryBase * time.Duration(attempt-1)
			slog.Debug("Login retry", "attempt", attempt, "wait", wait)
			time.Sleep(wait)
		}

		// Step 1: anonymous session
		anon, err := d.openSession(0)
		if err != nil {
			lastErr = err
			continue
		}
		slog.Debug("Anonymous session opened",
			"sessionId", anon.SessionInfo.SessionId,
			"userId", anon.SessionInfo.UserId)

		// Step 2: validate subscription (login)
		var val validateSubscriptionResponse
		err = d.apiPost("/Session/SessionService.svc/json/Validate_Subscription", map[string]interface{}{
			"Device":                 appName,
			"DownloadToken":          HILPOLTSTEINER_KURIER,
			"Email":                  d.username,
			"Password":               d.password,
			"SessionId":              anon.SessionInfo.SessionId,
			"UserId":                 anon.SessionInfo.UserId,
			"SubscriptionType":       subscriptionProvider,
			"AuthenticationProvider": "",
			"Version":                "1.0.0.0",
		}, &val)
		if err != nil {
			lastErr = fmt.Errorf("Validate_Subscription: %w", err)
			continue
		}

		if !isSuccess(val.Status) {
			if isTransient(val.StatusMessage) {
				lastErr = fmt.Errorf("login: backend unavailable (%s)", val.StatusMessage)
				slog.Warn("Login transient error, will retry",
					"attempt", attempt, "code", val.StatusMessage)
				continue
			}
			// Fatal: bad credentials, no subscription, etc.
			return fmt.Errorf("login failed: %s (%s)", val.StatusMessage, authHint(val.StatusMessage))
		}

		d.userID = val.UserID
		d.subscriptionID = val.SubscriptionID
		slog.Debug("Subscription validated",
			"userID", d.userID, "subscriptionID", d.subscriptionID)

		// Step 3: authenticated session
		auth, err := d.openSession(d.userID)
		if err != nil {
			lastErr = fmt.Errorf("authenticated OpenSession: %w", err)
			continue
		}
		d.sessionID = auth.SessionInfo.SessionId
		d.sessionExpires = time.Now().Add(20 * time.Hour)
		slog.Debug("Authenticated session opened",
			"sessionId", d.sessionID, "userId", auth.SessionInfo.UserId)
		return nil
	}

	return fmt.Errorf("login failed after %d attempts: %w", maxAuthRetries, lastErr)
}

// --- Issue listing ---

type contentPackage struct {
	ContentPackageId int    `json:"ContentPackageId"`
	PublicationDate  string `json:"PublicationDate"`
}

// ListIssues returns available issues for the given download token.
// No authentication required.
func (d *Donaukurier) ListIssues(downloadToken string) ([]*meta.Info, error) {
	var packages []contentPackage
	if err := d.apiGet(
		fmt.Sprintf("/Data/DataService.svc/getcontentpackagelist/%s/0/100", downloadToken),
		&packages,
	); err != nil {
		return nil, fmt.Errorf("ListIssues: %w", err)
	}

	var title string
	switch downloadToken {
	case HILPOLTSTEINER_KURIER:
		title = "Hilpoltsteiner Kurier"
	default:
		return nil, fmt.Errorf("unknown download token: %s", downloadToken)
	}

	var issues []*meta.Info
	for _, pkg := range packages {
		date, err := time.Parse("2006-01-02T15:04:05", pkg.PublicationDate)
		if err != nil {
			slog.Error("Failed to parse publication date",
				"date", pkg.PublicationDate, "error", err)
			continue
		}
		year := date.Year()
		issues = append(issues, &meta.Info{
			PublishingDate: meta.PublishingDate(date),
			Author:         "Donaukurier GmbH",
			Title:          title,
			Year:           &year,
			Language:       "de",
			Category:       meta.CategoryNewspaper,
			Annotations: map[string]any{
				AnnotationContentPackageID: pkg.ContentPackageId,
			},
		})
	}
	return issues, nil
}

// ShouldDownloadIssue reports whether an issue should be downloaded.
func ShouldDownloadIssue(lastBook *meta.Info, issue *meta.Info) bool {
	if lastBook == nil {
		return true
	}
	return issue.PublishingDate.Time().Compare(lastBook.PublishingDate.Time()) > 0
}

// --- Download ---

type contentPackagePublications struct {
	ContentPackagePublication []struct {
		PublicationID    int    `json:"PublicationID"`
		PublicationType  string `json:"PublicationType"`
		FullPdfAvailable bool   `json:"FullPdfAvailable"`
	} `json:"ContentPackagePublication"`
}

type requestDownloadResponse struct {
	Status     string `json:"Status"`
	OrderID    int    `json:"OrderID"`
	DownloadID int    `json:"DownloadID"`
}

type fullPDFResponse struct {
	Status        string `json:"Status"`
	StatusMessage string `json:"StatusMessage"`
	DownloadLink  string `json:"DownloadLink"`
}

func (d *Donaukurier) getPublicationID(cpID int) (int, error) {
	u := fmt.Sprintf(
		"%s/data/%d/data/GetContentPackagePublications-%d-V3.json",
		epaperBase, cpID, cpID,
	)
	resp, err := d.httpClient.Get(u)
	if err != nil {
		return 0, fmt.Errorf("getPublicationID: %w", err)
	}
	defer resp.Body.Close()
	var pubs contentPackagePublications
	if err := json.NewDecoder(resp.Body).Decode(&pubs); err != nil {
		return 0, fmt.Errorf("getPublicationID decode: %w", err)
	}
	for _, p := range pubs.ContentPackagePublication {
		if p.PublicationType == "Main" && p.FullPdfAvailable {
			return p.PublicationID, nil
		}
	}
	return 0, fmt.Errorf("no main publication with full PDF in content package %d", cpID)
}

// Download fetches the full PDF for the given issue and writes it to fPDF.
func (d *Donaukurier) Download(info *meta.Info, fPDF *os.File) error {
	cpID, ok := info.Annotations[AnnotationContentPackageID].(int)
	if !ok || cpID == 0 {
		return errors.New("content package ID not set in annotations")
	}

	if err := d.login(); err != nil {
		return err
	}

	pubID, err := d.getPublicationID(cpID)
	if err != nil {
		return err
	}
	slog.Debug("Got publication ID", "publicationID", pubID, "contentPackageID", cpID)

	// Authorise download
	var rd requestDownloadResponse
	if err := d.apiGet(fmt.Sprintf(
		"/Session/SessionService.svc/json/RequestDownload/%d/%d/%d/%d/0/Subscription",
		d.userID, d.sessionID, d.subscriptionID, cpID,
	), &rd); err != nil {
		return fmt.Errorf("RequestDownload: %w", err)
	}
	slog.Debug("RequestDownload", "status", rd.Status, "orderId", rd.OrderID, "downloadId", rd.DownloadID)

	// Confirm download (best-effort; failure is non-fatal)
	now := time.Now().UTC().Format("2006-01-02T15:04:05")
	if err := d.apiPost("/Session/SessionService.svc/json/Confirm_Download", map[string]interface{}{
		"UserId":    d.userID,
		"SessionId": d.sessionID,
		"Version":   platformVersion,
		"DownloadPublicationStatusHistory": map[string]interface{}{
			"PublicationId":                   cpID,
			"PublicationQuality":              "Full",
			"RequestedPublicationTitleFormat": "Newspaper",
			"StatusInfo":                      "",
			"StatusTime":                      now,
		},
		"DownloadId":     rd.DownloadID,
		"Device":         "WebApp",
		"DownloadStatus": "Started",
	}, nil); err != nil {
		slog.Warn("Confirm_Download failed (continuing)", "error", err)
	}

	// Get full PDF download link
	pdfPath := fmt.Sprintf(
		"/api/downloadservice/requestfullpdfdownload/%d/%d/%d/%d/%d/%d.json?_=%d",
		d.userID, d.sessionID, rd.OrderID, d.subscriptionID, cpID, pubID,
		time.Now().UnixMilli(),
	)
	var pr fullPDFResponse
	if err := d.apiGet(pdfPath, &pr); err != nil {
		return fmt.Errorf("requestFullPDFDownload: %w", err)
	}
	if pr.DownloadLink == "" {
		return fmt.Errorf("requestFullPDFDownload returned no link (status=%s msg=%s)",
			pr.Status, pr.StatusMessage)
	}
	slog.Debug("Got PDF download link", "url", pr.DownloadLink)

	// Fetch article/TOC metadata for bookmark improvement
	bookmarks, err := d.fetchBookmarks(cpID, pubID)
	if err != nil {
		slog.Warn("Failed to fetch bookmarks, skipping improvement", "error", err)
	}

	// Download PDF to a temp file so we can run pdftk over it
	fPDFTemp, err := os.CreateTemp("", "donaukurier-*.pdf")
	if err != nil {
		return fmt.Errorf("failed to create temp PDF file: %w", err)
	}
	defer func() { _ = os.Remove(fPDFTemp.Name()) }()

	resp, err := d.httpClient.Get(pr.DownloadLink)
	if err != nil {
		return fmt.Errorf("download PDF: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download PDF: status %d", resp.StatusCode)
	}
	if _, err := io.Copy(fPDFTemp, resp.Body); err != nil {
		return fmt.Errorf("write PDF: %w", err)
	}
	if err := fPDFTemp.Close(); err != nil {
		return fmt.Errorf("close temp PDF: %w", err)
	}
	slog.Debug("Downloaded PDF to temp file", "path", fPDFTemp.Name())

	if len(bookmarks) > 0 {
		// Prepend static cover bookmark
		bookmarks = append([]meta.Bookmark{
			{Title: "Titelseite", PageNumber: 1, Level: 1},
		}, bookmarks...)
		if err := meta.ReplaceBookmarks(fPDF, fPDFTemp.Name(), bookmarks); err != nil {
			return fmt.Errorf("failed to replace bookmarks: %w", err)
		}
	} else {
		// No bookmarks available; copy the raw PDF directly
		f, err := os.Open(fPDFTemp.Name())
		if err != nil {
			return fmt.Errorf("open temp PDF: %w", err)
		}
		if _, err := io.Copy(fPDF, f); err != nil {
			_ = f.Close()
			return fmt.Errorf("copy PDF: %w", err)
		}
		_ = f.Close()
	}

	if err := fPDF.Close(); err != nil {
		return fmt.Errorf("close PDF: %w", err)
	}

	slog.Info("Downloaded PDF", "date", info.PublishingDate, "path", fPDF.Name())
	return nil
}

// fetchBookmarks retrieves and parses article metadata from the epaper CDN
// static data files, returning bookmarks suitable for PDF injection.
func (d *Donaukurier) fetchBookmarks(cpID, pubID int) ([]meta.Bookmark, error) {
	u := fmt.Sprintf(
		"%s/data/%d/data/GetPublicationContentItems-%d.json",
		epaperBase, cpID, pubID,
	)
	resp, err := d.httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("fetch content items: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch content items: status %d", resp.StatusCode)
	}
	bookmarks, err := parseTocV2(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse content items: %w", err)
	}
	slog.Debug("Fetched bookmarks from content items", "count", len(bookmarks))
	return bookmarks, nil
}
