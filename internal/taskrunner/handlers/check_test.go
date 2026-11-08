package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"goto-bangumi/internal/apperrors"
	"goto-bangumi/internal/download"
	"goto-bangumi/internal/download/downloader"
	"goto-bangumi/internal/model"
)

type checkNetworkErrorDownloader struct {
	downloader.BaseDownloader
}

func (d *checkNetworkErrorDownloader) CheckHash(context.Context, string) (string, error) {
	return "", &apperrors.NetworkError{Err: errors.New("temporary failure")}
}

func TestCheckHandlerRetriesNetworkErrorWithoutReturningIt(t *testing.T) {
	dl := download.NewDownloadClient()
	dl.Downloader = &checkNetworkErrorDownloader{}
	handler := NewCheckHandler(nil, dl)
	task := model.NewAddTask(
		&model.Torrent{Link: "torrent", Name: "torrent"},
		model.NewBangumi(),
	)
	task.Guids = []string{"hash"}

	result := handler(context.Background(), task)

	if result.Err != nil {
		t.Fatalf("handler returned retryable error: %v", result.Err)
	}
	if result.PollAfter != 30*time.Second {
		t.Fatalf("PollAfter = %v, want 30s", result.PollAfter)
	}
}
