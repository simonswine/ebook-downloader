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
)

var (
	regexRessort  = regexp.MustCompile(`^[A-Z ]+$`)
	regexBookmark = regexp.MustCompile(`^\s*\d+\s+\|\s+`)
	regexIssue    = regexp.MustCompile(`^DER SPIEGEL (\d+)\. Jahrgang \| Heft (\d+) \| (\d+\.\d+.\d+)`)
)

type Bookmark struct {
	PageNumber int    `json:"page"`
	Level      int    `json:"level"`
	Title      string `json:"title"`
}

type TOC struct {
	Issue          int        `json:"issue"`
	Year           int        `json:"year"`
	PublishingDate time.Time  `json:"publishing_date"`
	Bookmarks      []Bookmark `json:"bookmarks"`
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
	Bookmark
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
				result.Bookmarks = append(result.Bookmarks, Bookmark{
					PageNumber: newBookmark.PageNumber,
					Level:      0,
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
					result.PublishingDate = publishingDate
				}

				continue
			}
		}

		if regexRessort.MatchString(text) {
			finish()
			lastRessort = text
			continue
		}

		if regexBookmark.MatchString(text) {
			finish()

			idx := strings.Index(text, "|")
			if idx < 0 {
				continue
			}

			page, err := strconv.Atoi(text[:idx-1])
			if err != nil {
				slog.Warn("Error parsing page number", "text", text, "error", err)
				continue
			}

			newBookmark = &parseBookmark{
				Bookmark: Bookmark{
					PageNumber: page,
					Level:      1,
				},
				titleParts: []string{text[idx+2:]},
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
