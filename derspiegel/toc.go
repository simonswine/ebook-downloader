package derspiegel

import (
	"bufio"
	"fmt"
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
	regexRessort        = regexp.MustCompile(`^[A-ZÄÖÜ ]+$`)
	regexBookmark       = regexp.MustCompile(`^\s*\d+\s+\|\s+`)
	regexBookmarkNoPipe = regexp.MustCompile(`^\s*(\d+)\s+(.+)$`)
	regexIssue          = regexp.MustCompile(`^DER SPIEGEL (\d+)\. Jahrgang \| Heft (\d+) \| (\d+\.\d+.\d+)`)
	regexIssueShort     = regexp.MustCompile(`^Nr\.\s+(\d+)\s+\|\s+(\d+\.\d+\.\d+)`)
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
		s = trimSpace(s)

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

// trimSpace removes regular whitespace and zero-width space characters
func trimSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}
		if r == '\u200b' {
			return true
		}
		/*|| r == '\u00ad' {
			return true
		}
		*/
		return false
	})
}

type tocParser interface {
	extractText(string, io.Writer) error
	parseTOC(io.Reader) (*TOC, error)
}

func getTocParser(info *meta.Info) tocParser {
	def := &toc202539{}
	if info == nil {
		return def
	}

	if info.Year == nil || *info.Year > 2025 {
		return def
	}

	if *info.Year == 2025 && (info.Issue == nil || *info.Issue > 39) {
		return def
	}

	return &tocLegacy{}
}

// parse toc before 2025-39
type tocLegacy struct {
}

func (t *tocLegacy) extractText(path string, buf io.Writer) error {
	if err := meta.ExtractText(path, 4, 5, buf); err != nil {
		return fmt.Errorf("failed to extract text: %w", err)
	}
	return nil
}

func (t *tocLegacy) parseTOC(r io.Reader) (*TOC, error) {
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
			newBookmark.Title = trimSpace(newBookmark.titleParts.String())
			result.Bookmarks = append(result.Bookmarks, newBookmark.Bookmark)
			lastRessort = ""
			newBookmark = nil
		}
	)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())

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
					d := meta.PublishingDate(publishingDate)
					result.PublishingDate = &d
				}

				continue
			}
		}

		if regexRessort.MatchString(text) && text != "DER SPIEGEL" && text != "DER" && text != "SPIEGEL" {
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
				Bookmark: meta.Bookmark{
					PageNumber: page,
					Level:      2,
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

type toc202539 struct {
}

// rects with toc starting issue 2025-39 new layout of toc
var rects202539 = []meta.Rectangle{
	{Page: 1, X: 490, Y: 50, Width: 90, Height: 20},
	{Page: 4, X: 30, Y: 120, Width: 130, Height: 400, Prefix: "\n"},
	{Page: 4, X: 450, Y: 120, Width: 130, Height: 400, Prefix: "\n"},
	{Page: 5, X: 25, Y: 70, Width: 130, Height: 700, Prefix: "\n"},
	{Page: 5, X: 440, Y: 70, Width: 130, Height: 600, Prefix: "\n"},
	{Page: 5, X: 440, Y: 670, Width: 130, Height: 100, Prefix: "\nMETA\n"},
}

func (t *toc202539) extractText(path string, buf io.Writer) error {
	if err := meta.ExtractTextRectangles(path, rects202539, buf); err != nil {
		return fmt.Errorf("failed to extract text: %w", err)
	}
	return nil
}

func (_ *toc202539) parseTOC(r io.Reader) (*TOC, error) {
	var result TOC
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
			newBookmark.Title = trimSpace(newBookmark.titleParts.String())
			result.Bookmarks = append(result.Bookmarks, newBookmark.Bookmark)
			lastRessort = ""
			newBookmark = nil
		}
	)

	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())

		// Parse issue number and date from "Nr. 7 | 6.2.2026" format
		if m := regexIssueShort.FindStringSubmatch(text); len(m) == 3 {
			issue, err := strconv.Atoi(m[1])
			if err != nil {
				slog.Warn("Error parsing issue", "text", m[1], "error", err)
			} else {
				result.Issue = issue
			}

			publishingDate, err := time.Parse("2.1.2006", m[2])
			if err != nil {
				slog.Warn("Error parsing publishing date", "text", m[2], "error", err)
			} else {
				d := meta.PublishingDate(publishingDate)
				result.PublishingDate = &d
				result.Year = publishingDate.Year()
			}
			continue
		}

		if regexRessort.MatchString(text) && text != "DER SPIEGEL" && text != "DER" && text != "SPIEGEL" {
			finish()
			lastRessort = text
			continue
		}

		if m := regexBookmarkNoPipe.FindStringSubmatch(text); len(m) == 3 {
			finish()

			page, err := strconv.Atoi(m[1])
			if err != nil {
				slog.Warn("Error parsing page number", "text", text, "error", err)
				continue
			}

			newBookmark = &parseBookmark{
				Bookmark: meta.Bookmark{
					PageNumber: page,
					Level:      2,
				},
				titleParts: []string{m[2]},
			}
			continue
		}

		// finally handle it as additional text lines
		if newBookmark != nil && text != "" {
			newBookmark.titleParts = append(newBookmark.titleParts, text)
		}
	}
	if scanner.Err() != nil {
		return nil, scanner.Err()
	}
	finish()

	return &result, nil
}
