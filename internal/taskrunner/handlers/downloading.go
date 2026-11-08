package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"goto-bangumi/internal/apperrors"
	"goto-bangumi/internal/database"
	"goto-bangumi/internal/download"
	"goto-bangumi/internal/model"
	"goto-bangumi/internal/taskrunner"
)

// NewDownloadingHandler 创建下载监控处理器，合并进度检查和 ETA 计算
func NewDownloadingHandler(db *database.DB, dl *download.DownloadClient) taskrunner.PhaseFunc {
	return func(ctx context.Context, task *model.Task) taskrunner.PhaseResult {
		// 检查是否超时（4小时）
		if time.Since(task.StartTime) > 4*time.Hour {
			slog.Warn("[downloading handler] 下载超过4小时，标记为异常",
				"torrent", task.Torrent.Name,
				"duid", task.Torrent.DownloadUID,
				"elapsed", time.Since(task.StartTime))

			db.AddTorrentError(ctx, task.Torrent.Link)
			return taskrunner.PhaseResult{Err: fmt.Errorf("download timeout after 4 hours")}
		}

		// 获取种子信息
		info, err := dl.GetTorrentInfo(ctx, task.Torrent.DownloadUID)
		if err != nil {
			slog.Error("[downloading handler] 获取种子信息失败",
				"error", err, "duid", task.Torrent.DownloadUID)
			if apperrors.IsNetworkError(err) {
				return taskrunner.PhaseResult{PollAfter: 30 * time.Second}
			}
			return taskrunner.PhaseResult{Err: err}
		}

		if info == nil {
			slog.Warn("[downloading handler] 种子不存在", "duid", task.Torrent.DownloadUID)
			return taskrunner.PhaseResult{Err: fmt.Errorf("torrent not found")}
		}

		// 检查是否下载完成（Completed > 0 表示已完成，为 Unix 时间戳）
		if info.Completed > 0 {
			task.Torrent.Downloaded = model.DownloadDone
			if err := db.AddTorrentDownload(ctx, task.Torrent.Link); err != nil {
				slog.Error("[downloading handler] 更新种子状态失败", "error", err)
				return taskrunner.PhaseResult{Err: err}
			}

			slog.Info("[downloading handler] 下载完成", "torrent", task.Torrent.Name)
			return taskrunner.PhaseResult{} // 成功，进入下一阶段
		}

		// 未完成，根据 ETA 自适应轮询
		interval := calculateEta(int64(info.ETA))
		slog.Debug("[downloading handler] 设置检查间隔",
			"torrent", task.Torrent.Name,
			"eta", info.ETA,
			"interval", interval)

		return taskrunner.PhaseResult{PollAfter: time.Duration(interval) * time.Second}
	}
}

// calculateEta 根据 ETA 计算检查间隔（秒）
func calculateEta(eta int64) int {
	if eta <= 0 || eta < 60 {
		return 10
	}
	if eta < 300 {
		return 30
	}
	if eta < 1800 {
		return 120
	}
	return 300
}
