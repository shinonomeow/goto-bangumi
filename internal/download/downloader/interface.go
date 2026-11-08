package downloader

import (
	"context"
	"log/slog"
	"strings"

	"goto-bangumi/internal/model"
)

// BaseDownloader 定义下载器的基础接口
// 主要实现以下功能:
// 1. renamer,用以实现重命名功能 hash, old name, new name
// 2. move, 用以实现改数据后的转移
// 3. add, 用以加种子
// 4. get_info, 获取当前所有的有关种子
// 5. check, 启动时检查连通性和 token, 并刷新下载器信息
type BaseDownloader interface {
	// Init 初始化下载器
	Init(config *model.DownloaderConfig) error

	// Check 启动时调用一次, 检查主机连通性和 API token, 并刷新下载器信息
	Check(ctx context.Context) error

	// GetTorrentFiles 获取种子的所有文件列表
	GetTorrentFiles(ctx context.Context, hash string) ([]string, error)

	// GetTorrentInfo 获取单个种子的信息
	GetTorrentInfo(ctx context.Context, hash string) (*model.TorrentDownloadInfo, error)

	// TorrentsInfo 获取种子信息列表
	TorrentsInfo(ctx context.Context, statusFilter, category string, tag *string, limit int) ([]map[string]any, error)

	// Rename 重命名种子文件
	Rename(ctx context.Context, torrentHash, oldPath, newPath string) (bool, error)

	// Move 移动种子到新位置
	Move(ctx context.Context, hashes []string, newLocation string) (bool, error)

	// Add 添加种子
	Add(ctx context.Context, torrentInfo *model.TorrentInfo, savePath string) ([]string, error)

	// CheckHash 检查种子是否存在，返回真实的hash
	CheckHash(ctx context.Context, hash string) (string, error)

	// Delete 删除种子
	Delete(ctx context.Context, hashes []string) (bool, error)
}

// NewDownloader 创建下载器实例
// 根据 downloaderType 动态选择具体的下载器实现
// 支持的类型: "qbittorrent", "qb"
func NewDownloader(downloaderType string) BaseDownloader {

	var d BaseDownloader

	// 根据类型选择具体的下载器实现
	switch strings.ToLower(downloaderType) {
	case "qbittorrent":
		d = NewQBittorrentDownloader()
	case "mock":
		d = NewMockDownloader()
	case "clouddrive", "clouddrive2":
		d = NewCloudDriveDownloader()
	default:
		slog.Warn("未知的下载器类型，使用默认的 qBittorrent 下载器", "type", downloaderType)
		return NewQBittorrentDownloader()
	}
	return d
}
