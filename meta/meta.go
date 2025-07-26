package meta

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	DATE_FORMAT = "2006-01-02"
)

type Category string

const (
	CategoryMagazine  Category = "Magazine"
	CategoryBook      Category = "Book"
	CategoryNewspaper Category = "Newspaper"
)

type PublishingDate time.Time

func (d PublishingDate) String() string {
	return d.Format(DATE_FORMAT)
}

func (d PublishingDate) Time() time.Time {
	return time.Time(d)
}

func (d PublishingDate) Format(layout string) string {
	return time.Time(d).Format(layout)
}

func (d PublishingDate) MarshalJSON() ([]byte, error) {
	return time.Time(d).MarshalJSON()
}

func (d *PublishingDate) UnmarshalJSON(data []byte) error {
	t := time.Time(*d)
	return t.UnmarshalJSON(data)
}

type Info struct {
	Issue          *int           `json:"issue"`
	Year           *int           `json:"year"`
	PublishingDate PublishingDate `json:"publishing_date"`
	Author         string         `json:"author"`
	Title          string         `json:"title"`
	Subtitle       *string        `json:"subtitle"`
	Language       string         `json:"language"`
	Category       Category       `json:"category"`
	Keywords       []string       `json:"keywords"`
	Annotations    map[string]any `json:"annotation"`
}

func (i *Info) Series() string {
	if i.Year == nil {
		return i.Title
	}
	return fmt.Sprintf("%s %04d", i.Title, *i.Year)
}

func WriteEbookMeta(path string, info *Info) error {
	bufErr := bytes.NewBuffer(nil)

	args := make([]string, 0, 64)
	args = append(args, "ebook-meta", path)

	if info.Issue != nil {
		args = append(args, "--index", strconv.Itoa(*info.Issue))
	}

	if info.Year != nil {
		args = append(args, "--series", info.Series())
	}

	subtitle := ""
	if info.Subtitle != nil {
		subtitle = fmt.Sprintf(": %s", *info.Subtitle)
	}

	switch info.Category {
	case CategoryMagazine:
		args = append(args, "--category", "Magazine")
		args = append(args, "--title", fmt.Sprintf("%s Nr. %02d/%04d%s", info.Title, *info.Issue, *info.Year, subtitle))
		args = append(args, "--title-sort", fmt.Sprintf("%s %04d-%02d%s", info.Title, *info.Year, *info.Issue, subtitle))
	case CategoryNewspaper:
		args = append(args, "--category", "Newspaper")
		args = append(args, "--title", fmt.Sprintf("%s vom %s%s", info.Title, info.PublishingDate.Format("02.01.2006"), subtitle))
		args = append(args, "--title-sort", fmt.Sprintf("%s %s%s", info.Title, info.PublishingDate.Format("2006-01-02"), subtitle))
	default:
		return fmt.Errorf("unknown category: %s", info.Category)
	}

	args = append(args, "--authors", info.Author)
	args = append(args, "--author-sort", info.Author)
	args = append(args, "--publisher", info.Author)
	args = append(args, "--language", info.Language)
	args = append(args, "--date", info.PublishingDate.Format("2006-01-02"))

	slog.Debug("Updating meta", "cmd", fmt.Sprintf("%q", args))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stderr = bufErr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to update meta: %w, stderr: %s", err, bufErr.String())
	}

	return nil
}

type Bookmark struct {
	PageNumber int    `json:"page"`
	Level      int    `json:"level"`
	Title      string `json:"title"`
}

// Replace all existing bookmarks in a pdf, by shelling out to pdftk
func ReplaceBookmarks(out io.Writer, path string, bookmarks []Bookmark) error {
	var sb strings.Builder
	// write bookmarks to temp file
	for _, b := range bookmarks {
		sb.WriteString("BookmarkBegin\n")
		sb.WriteString("BookmarkTitle: ")
		sb.WriteString(b.Title)
		sb.WriteString("\n")
		sb.WriteString("BookmarkLevel: ")
		sb.WriteString(strconv.Itoa(b.Level))
		sb.WriteString("\n")
		sb.WriteString("BookmarkPageNumber: ")
		sb.WriteString(strconv.Itoa(b.PageNumber))
		sb.WriteString("\n")
	}

	// use pdftk to add bookmarks
	cmd := exec.Command("pdftk", path, "update_info_utf8", "-", "output", "-")
	cmd.Stdin = strings.NewReader(sb.String())
	cmd.Stdout = out
	bufErr := bytes.NewBuffer(nil)
	cmd.Stderr = bufErr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running pdftk command: %w, stderr: %s", err, bufErr.String())
	}

	return nil
}

func ExtractText(path string, from, to int, out io.Writer) error {
	bufErr := bytes.NewBuffer(nil)
	cmd := exec.Command("pdftotext", "-f", strconv.Itoa(from), "-l", strconv.Itoa(to), "-raw", path, "-")
	cmd.Stdout = out
	cmd.Stderr = bufErr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running pdftostring command: %w, stderr: %s", err, bufErr.String())
	}
	return nil
}
