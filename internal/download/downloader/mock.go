package downloader

import (
	"context"
	"fmt"
	"sync"

	"goto-bangumi/internal/apperrors"
	"goto-bangumi/internal/model"
)

// mockTorrent 模拟种子的内部状态
type mockTorrent struct {
	hash       string
	name       string
	info       *model.TorrentDownloadInfo
	files      []string
	queryCount int
	category   string
	tags       string
}

// MockDownloader 有状态的模拟下载器，用于测试
type MockDownloader struct {
	config              *model.DownloaderConfig
	mu                  sync.RWMutex
	torrents            map[string]*mockTorrent
	completionThreshold int
}

// NewMockDownloader 创建新的模拟下载器
func NewMockDownloader() *MockDownloader {
	return &MockDownloader{
		completionThreshold: 3,
	}
}

// Init 初始化下载器，加载预置数据
func (d *MockDownloader) Init(config *model.DownloaderConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.config = config
	d.torrents = make(map[string]*mockTorrent)

	// 加载预置数据
	for hash, info := range MockTorrentInfos {
		files := MockFiles[hash]
		d.torrents[hash] = &mockTorrent{
			hash: hash,
			name: hash,
			info: &model.TorrentDownloadInfo{
				ETA:       info.ETA,
				SavePath:  info.SavePath,
				Completed: info.Completed,
			},
			files:      files,
			queryCount: d.completionThreshold, // 预置数据初始即完成
		}
	}
	return nil
}

// Check 检查下载器状态
func (d *MockDownloader) Check(ctx context.Context) error {
	return nil
}

// Add 添加种子
func (d *MockDownloader) Add(ctx context.Context, torrentInfo *model.TorrentInfo, savePath string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	mt := &mockTorrent{
		name: torrentInfo.Name,
		info: &model.TorrentDownloadInfo{
			ETA:       300,
			SavePath:  savePath,
			Completed: 0,
		},
		files:      []string{fmt.Sprintf("[Mock] %s.mp4", torrentInfo.Name)},
		queryCount: 0,
	}

	hashes := make([]string, 0, 2)
	if torrentInfo.InfoHashV1 != "" {
		mt.hash = torrentInfo.InfoHashV1
		d.torrents[torrentInfo.InfoHashV1] = mt
		hashes = append(hashes, torrentInfo.InfoHashV1)
	}
	if torrentInfo.InfoHashV2 != "" {
		v2Hash := torrentInfo.InfoHashV2
		if len(v2Hash) > 40 {
			v2Hash = v2Hash[:40]
		}
		if mt.hash == "" {
			mt.hash = v2Hash
		}
		d.torrents[v2Hash] = mt // 同一个对象
		hashes = append(hashes, v2Hash)
	}
	return hashes, nil
}

// Delete 删除种子
func (d *MockDownloader) Delete(ctx context.Context, hashes []string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, h := range hashes {
		// 找到要删除的 torrent，同时清理所有指向同一对象的 key
		if mt, ok := d.torrents[h]; ok {
			for k, v := range d.torrents {
				if v == mt {
					delete(d.torrents, k)
				}
			}
		}
	}
	return true, nil
}

// GetTorrentInfo 获取单个种子详细信息，自动推进下载进度
func (d *MockDownloader) GetTorrentInfo(ctx context.Context, hash string) (*model.TorrentDownloadInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	mt, ok := d.torrents[hash]
	if !ok {
		return nil, nil
	}

	mt.queryCount++
	if mt.queryCount >= d.completionThreshold {
		mt.info.Completed = 1
		mt.info.ETA = 0
	} else {
		eta := 300 - mt.queryCount*100
		eta = max(eta, 0)
		mt.info.ETA = eta
	}

	// 返回副本
	return &model.TorrentDownloadInfo{
		ETA:       mt.info.ETA,
		SavePath:  mt.info.SavePath,
		Completed: mt.info.Completed,
	}, nil
}

// GetTorrentFiles 获取种子文件列表
func (d *MockDownloader) GetTorrentFiles(ctx context.Context, hash string) ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	mt, ok := d.torrents[hash]
	if !ok {
		return []string{}, nil
	}
	return mt.files, nil
}

// TorrentsInfo 获取种子信息列表
func (d *MockDownloader) TorrentsInfo(ctx context.Context, statusFilter, category string, tag *string, limit int) ([]map[string]any, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []map[string]any
	for hash, mt := range d.torrents {
		// 按 category 过滤
		if category != "" && mt.category != category {
			continue
		}
		// 按 tag 过滤
		if tag != nil && mt.tags != *tag {
			continue
		}
		// 按 statusFilter 过滤
		if statusFilter != "" {
			if statusFilter == "completed" && mt.info.Completed != 1 {
				continue
			}
			if statusFilter == "downloading" && mt.info.Completed != 0 {
				continue
			}
		}

		result = append(result, map[string]any{
			"hash":      hash,
			"name":      mt.name,
			"category":  mt.category,
			"tags":      mt.tags,
			"save_path": mt.info.SavePath,
			"completed": mt.info.Completed,
			"eta":       mt.info.ETA,
		})

		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

// CheckHash 检查种子是否存在
func (d *MockDownloader) CheckHash(ctx context.Context, hash string) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if _, ok := d.torrents[hash]; ok {
		return hash, nil
	}
	return "", &apperrors.DownloadKeyError{
		Err: fmt.Errorf("种子不存在"),
		Key: hash,
	}
}

// Rename 重命名种子文件
func (d *MockDownloader) Rename(ctx context.Context, torrentHash, oldPath, newPath string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	mt, ok := d.torrents[torrentHash]
	if !ok {
		return true, nil
	}
	for i, f := range mt.files {
		if f == oldPath {
			mt.files[i] = newPath
			break
		}
	}
	return true, nil
}

// Move 移动种子到新位置
func (d *MockDownloader) Move(ctx context.Context, hashes []string, newLocation string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, h := range hashes {
		if mt, ok := d.torrents[h]; ok {
			mt.info.SavePath = newLocation
		}
	}
	return true, nil
}

// AddMockTorrent 手动添加测试种子数据
func (d *MockDownloader) AddMockTorrent(hash string, info *model.TorrentDownloadInfo, files []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.torrents[hash] = &mockTorrent{
		hash: hash,
		name: hash,
		info: &model.TorrentDownloadInfo{
			ETA:       info.ETA,
			SavePath:  info.SavePath,
			Completed: info.Completed,
		},
		files:      files,
		queryCount: d.completionThreshold,
	}
}
