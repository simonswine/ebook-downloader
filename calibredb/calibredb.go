package calibredb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/simonswine/ebook-downloader/meta"
)

func New(path string) *CalibreDB {
	return &CalibreDB{
		path: path,
	}
}

type CalibreDB struct {
	path string
}

// Add a book to the database
func (c *CalibreDB) AddBook(path string) error {
	bufOut := bytes.NewBuffer(nil)
	bufErr := bytes.NewBuffer(nil)

	cmdStr := []string{
		"calibredb",
		"add",
		"--with-library", c.path,
		path,
	}
	slog.Debug("Adding book to database", "cmd", strings.Join(cmdStr, " "))

	cmd := exec.Command(cmdStr[0], cmdStr[1:]...)
	cmd.Stdout = bufOut
	cmd.Stderr = bufErr
	return cmd.Run()
}

type floatToInt int

func (f *floatToInt) UnmarshalJSON(data []byte) error {
	var raw interface{}
	err := json.Unmarshal(data, &raw)
	if err != nil {
		return err
	}

	switch raw := raw.(type) {
	case int:
		*f = floatToInt(raw)
	case float64:
		*f = floatToInt(raw)
	default:
		return fmt.Errorf("unknown type %T", raw)
	}
	return nil
}

// This gets the last publication date of the book added to the db in the series
func (c *CalibreDB) LastBook(series string) (*meta.Info, error) {
	bufOut := bytes.NewBuffer(nil)
	bufErr := bytes.NewBuffer(nil)

	cmdStr := []string{
		"calibredb",
		"list",
		"--with-library", c.path,
		"--for-machine",
		"--fields", "series,series_index,pubdate",
		"--search", fmt.Sprintf(`series:"%s"`, series),
		"--sort-by", "pubdate",
		"--limit", "1",
	}
	slog.Debug("searchling last book in database", "cmd", strings.Join(cmdStr, " "))

	cmd := exec.Command(cmdStr[0], cmdStr[1:]...)
	cmd.Stdout = bufOut
	cmd.Stderr = bufErr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to find last book in database '%s': %w", bufErr.String(), err)
	}

	data := []struct {
		ID          int        `json:"id"`
		PubDate     time.Time  `json:"pubdate"`
		Series      string     `json:"series"`
		SeriesIndex floatToInt `json:"series_index"`
	}{}
	if err := json.Unmarshal(bufOut.Bytes(), &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	b := data[0]
	slog.Debug("found last book in database", "series", b.Series, "series_index", b.SeriesIndex, "pubdate", b.PubDate.Format(meta.DATE_FORMAT))
	seriesIndex := int(b.SeriesIndex)
	var year *int
	if len(b.Series) > 4 {
		yearInt, err := strconv.Atoi(b.Series[len(b.Series)-4 : len(b.Series)])
		if err == nil {
			year = &yearInt
		} else {
			panic(err)
		}
	}
	return &meta.Info{
		PublishingDate: meta.PublishingDate(b.PubDate),
		Issue:          &seriesIndex,
		Year:           year,
	}, nil
}
