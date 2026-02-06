package donaukurier

import (
	"testing"
	"time"

	"github.com/simonswine/ebook-downloader/meta"
	"github.com/stretchr/testify/require"
)

// Test_ShouldDownloadIssue tests the logic for determining whether an issue should be downloaded
// This encapsulates the nil-safety logic from the sync command
func Test_ShouldDownloadIssue(t *testing.T) {
	date1 := meta.PublishingDate(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	date2 := meta.PublishingDate(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
	date3 := meta.PublishingDate(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))

	tests := []struct {
		name         string
		lastBook     *meta.Info
		issue        *meta.Info
		wantDownload bool
	}{
		{
			name:         "no books in database - should download",
			lastBook:     nil,
			issue:        &meta.Info{PublishingDate: date2},
			wantDownload: true,
		},
		{
			name:         "issue is newer than lastBook - should download",
			lastBook:     &meta.Info{PublishingDate: date1},
			issue:        &meta.Info{PublishingDate: date3},
			wantDownload: true,
		},
		{
			name:         "issue is same date as lastBook - should not download",
			lastBook:     &meta.Info{PublishingDate: date2},
			issue:        &meta.Info{PublishingDate: date2},
			wantDownload: false,
		},
		{
			name:         "issue is older than lastBook - should not download",
			lastBook:     &meta.Info{PublishingDate: date3},
			issue:        &meta.Info{PublishingDate: date1},
			wantDownload: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldDownloadIssue(tt.lastBook, tt.issue)
			require.Equal(t, tt.wantDownload, got)
		})
	}
}
