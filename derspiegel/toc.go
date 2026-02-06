package derspiegel

import (
	"bufio"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/simonswine/ebook-downloader/meta"
)

var (
	regexRessort        = regexp.MustCompile(`^[A-Z ]+$`)
	regexBookmark       = regexp.MustCompile(`^\s*\d+\s+\|\s+`)
	regexBookmarkSimple = regexp.MustCompile(`^\s*(\d+)\s+[A-Z]`) // bookmark without pipe, followed by uppercase letter
	regexIssue          = regexp.MustCompile(`^DER SPIEGEL (\d+)\. Jahrgang \| Heft (\d+) \| (\d+\.\d+.\d+)`)
	regexIssueSimple    = regexp.MustCompile(`^DER SPIEGEL (\d+) \| (\d+)$`) // simple format: issue | year
)

type TOC struct {
	Issue          int                  `json:"issue"`
	Year           int                  `json:"year"`
	PublishingDate *meta.PublishingDate `json:"publishing_date,omitempty"`
	Bookmarks      []meta.Bookmark      `json:"bookmarks"`
}

type titleParts []string

func isLower(r rune) bool {
	if r == 'ü' || r == 'ß' || r == 'ä' || r == 'ö' {
		return true
	}
	return unicode.IsLower(r)
}

func (c titleParts) String() string {
	var result string
	for i, s := range c {

		// trim spaces
		s = strings.TrimSpace(s)

		// handle hyphenation
		if i > 0 && len(result) > 0 {
			if result[len(result)-1] == '-' {
				if isLower([]rune(s)[0]) {
					result = result[:len(result)-1]
				}
			} else {
				result += " "
			}
		}
		result += s
	}
	result = strings.ReplaceAll(result, "\u00ad ", "")
	result = strings.ReplaceAll(result, "\u00ad", "")
	return result
}

type parseBookmark struct {
	meta.Bookmark
	titleParts titleParts
}

func parseTOC(r io.Reader) (*TOC, error) {
	var result TOC
	// go through result line by line
	scanner := bufio.NewScanner(r)
	var (
		newBookmark *parseBookmark
		lastRessort string
		finish      = func() {
			if newBookmark == nil {
				return
			}

			// If we have a ressort, we need to add it to the list.
			if lastRessort != "" {
				result.Bookmarks = append(result.Bookmarks, meta.Bookmark{
					PageNumber: newBookmark.PageNumber,
					Level:      1,
					Title:      lastRessort,
				})
			}
			newBookmark.Title = newBookmark.titleParts.String()
			result.Bookmarks = append(result.Bookmarks, newBookmark.Bookmark)
			lastRessort = ""
			newBookmark = nil
		}
	)
	for scanner.Scan() {
		text := scanner.Text()

		if strings.HasPrefix(text, "DER SPIEGEL") {
			// Try the full format first: "DER SPIEGEL 79. Jahrgang | Heft 28 | 4.7.2025"
			if m := regexIssue.FindStringSubmatch(text); len(m) == 4 {
				year, err := strconv.Atoi(m[1])
				if err != nil {
					slog.Warn("Error parsing year", "text", m[1], "error", err)
				} else {
					result.Year = year + 1946
				}

				issue, err := strconv.Atoi(m[2])
				if err != nil {
					slog.Warn("Error parsing issue", "text", m[2], "error", err)
				} else {
					result.Issue = issue
				}

				publishingDate, err := time.Parse("2.1.2006", m[3])
				if err != nil {
					slog.Warn("Error parsing publishing date", "text", m[3], "error", err)
				} else {
					result.PublishingDate = meta.PublishingDate(publishingDate)
				}

				continue
			}
			// Try the simple format: "DER SPIEGEL 7 | 2026"
			if m := regexIssueSimple.FindStringSubmatch(text); len(m) == 3 {
				issue, err := strconv.Atoi(m[1])
				if err != nil {
					slog.Warn("Error parsing issue", "text", m[1], "error", err)
				} else {
					result.Issue = issue
				}

				year, err := strconv.Atoi(m[2])
				if err != nil {
					slog.Warn("Error parsing year", "text", m[2], "error", err)
				} else {
					result.Year = year
				}

				continue
			}
		}

		if regexRessort.MatchString(text) && text != "DER SPIEGEL" && text != "DER" && text != "SPIEGEL" {
			finish()
			lastRessort = text
			continue
		}

		// Try bookmark with pipe: "8 | Aufrüstung Bunker bauen,"
		if regexBookmark.MatchString(text) {
			finish()

			idx := strings.Index(text, "|")
			if idx < 0 {
				continue
			}

			page, err := strconv.Atoi(strings.TrimSpace(text[:idx]))
			if err != nil {
				slog.Warn("Error parsing page number", "text", text, "error", err)
				continue
			}

			newBookmark = &parseBookmark{
				Bookmark: meta.Bookmark{
					PageNumber: page,
					Level:      2,
				},
				titleParts: []string{text[idx+2:]},
			}
			continue
		}

		// Try bookmark without pipe: "8 Skandale Sexualstraftäter"
		if m := regexBookmarkSimple.FindStringSubmatch(text); len(m) >= 2 {
			finish()

			page, err := strconv.Atoi(m[1])
			if err != nil {
				slog.Warn("Error parsing page number", "text", text, "error", err)
				continue
			}

			// Extract the title after the page number
			titleStart := len(m[1])
			for titleStart < len(text) && text[titleStart] == ' ' {
				titleStart++
			}

			newBookmark = &parseBookmark{
				Bookmark: meta.Bookmark{
					PageNumber: page,
					Level:      2,
				},
				titleParts: []string{text[titleStart:]},
			}
			continue
		}

		// when lines are longer unlikely a bookmark, we
		if len(text) > 40 {
			finish()
			continue
		}

		// finally handle it as additional text lines
		if newBookmark != nil {
			newBookmark.titleParts = append(newBookmark.titleParts, text)
		}

	}
	if scanner.Err() != nil {
		return nil, scanner.Err()
	}
	finish()

	return &result, nil
}
