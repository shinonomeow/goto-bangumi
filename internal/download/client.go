// Package download 提供下载客户端，转发下载器操作
package download

import (
	"context"
	"fmt"
	"strings"

	"goto-bangumi/internal/download/downloader"
	"goto-bangumi/internal/model"
	"goto-bangumi/internal/network"
)

// DownloadClient 下载客户端
type DownloadClient struct {
	Downloader     downloader.BaseDownloader
	SavePath       string
	downloaderType string
}

// NewDownloadClient 创建下载客户端实例
func NewDownloadClient() *DownloadClient {
	return &DownloadClient{}
}

func (c *DownloadClient) Init(config *model.DownloaderConfig) {
	c.SavePath = config.SavePath

	downloaderType := strings.ToLower(config.Type)
	if c.downloaderType != downloaderType {
		c.downloaderType = downloaderType
		dl := downloader.NewDownloader(c.downloaderType)
		c.Downloader = dl
	}
	c.Downloader.Init(config)
}

// CheckConnection 启动时检查下载器的连通性和 token, 并刷新下载器信息
func (c *DownloadClient) CheckConnection(ctx context.Context) error {
	return c.Downloader.Check(ctx)
}

func (c *DownloadClient) Check(ctx context.Context, hash string) (string, error) {
	return c.Downloader.CheckHash(ctx, hash)
}

// Add 添加种子
func (c *DownloadClient) Add(ctx context.Context, url, savePath string) ([]string, error) {
	// 解析种子或磁力链接
	var torrentInfo *model.TorrentInfo
	var err error

	if !strings.HasPrefix(url, "magnet:") {
		// 下载种子文件
		networkClient := network.GetRequestClient()
		respBody, err := networkClient.Get(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("[download client] 下载种子文件失败: %w", err)
		}

		// 解析种子文件
		torrentInfo, err = ParseTorrent(respBody)
		if err != nil {
			return nil, fmt.Errorf("[download client] 解析种子文件失败: %w", err)
		}
	} else {
		// 解析磁力链接
		torrentInfo, err = ParseTorrentURL(url)
		if err != nil {
			return nil, fmt.Errorf("[download client] 解析磁力链接失败: %w", err)
		}
	}

	// 3. 调用实际方法
	hashs, err := c.Downloader.Add(ctx, torrentInfo, savePath)
	if err != nil {
		return nil, err
	}
	// TODO: 要对拿回来的 做一个 check, 会有很多情况, 比如有 v2但是qb 不认,所以这传回来的应该是个 list
	// 然后通过一些可能的 check , 来确认到底是哪一个
	// 5. check hash 拿到 真实的 hash
	// duid, err := c.Downloader.CheckHash(url)

	return hashs, err
}

// Delete 删除种子
func (c *DownloadClient) Delete(ctx context.Context, hashes []string) error {
	_, err := c.Downloader.Delete(ctx, hashes)
	return err
}

// Rename 重命名种子文件
func (c *DownloadClient) Rename(ctx context.Context, hash, oldPath, newPath string) error {
	_, err := c.Downloader.Rename(ctx, hash, oldPath, newPath)
	return err
}

// Move 移动种子
func (c *DownloadClient) Move(ctx context.Context, hashes []string, location string) error {
	_, err := c.Downloader.Move(ctx, hashes, location)
	return err
}

// GetTorrentFiles 获取种子文件列表
func (c *DownloadClient) GetTorrentFiles(ctx context.Context, hash string) ([]string, error) {
	return c.Downloader.GetTorrentFiles(ctx, hash)
}

func (c *DownloadClient) GetTorrentInfo(ctx context.Context, hash string) (*model.TorrentDownloadInfo, error) {
	return c.Downloader.GetTorrentInfo(ctx, hash)
}

// TorrentsInfo 获取种子信息列表
func (c *DownloadClient) TorrentsInfo(ctx context.Context, statusFilter, category string, tag *string, limit int) ([]map[string]any, error) {
	return c.Downloader.TorrentsInfo(ctx, statusFilter, category, tag, limit)
}
