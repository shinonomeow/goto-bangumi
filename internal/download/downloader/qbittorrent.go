package downloader

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"goto-bangumi/internal/apperrors"
	"goto-bangumi/internal/model"

	"github.com/go-resty/resty/v2"
)

// QBAPI qBittorrent API URL 定义
var QBAPI = map[string]string{
	"add":            "/api/v2/torrents/add",
	"addTags":        "/api/v2/torrents/addTags",
	"createCategory": "/api/v2/torrents/createCategory",
	"delete":         "/api/v2/torrents/delete",
	"getFiles":       "/api/v2/torrents/files",
	"info":           "/api/v2/torrents/info",
	"properties":     "/api/v2/torrents/properties",
	"renameFile":     "/api/v2/torrents/renameFile",
	"setCategory":    "/api/v2/torrents/setCategory",
	"setLocation":    "/api/v2/torrents/setLocation",
	"setPreferences": "/api/v2/app/setPreferences",
	"version":        "/api/v2/app/version",
}

// QBittorrentDownloader qBittorrent 下载器实现
type QBittorrentDownloader struct {
	client      *resty.Client
	config      *model.DownloaderConfig
	ConfigName  string
	APIInterval int // API 调用间隔（毫秒）
	limiter     *apiLimiter
}

// NewQBittorrentDownloader 创建新的 qBittorrent 下载器
func NewQBittorrentDownloader() *QBittorrentDownloader {
	return &QBittorrentDownloader{
		client:      resty.New(),
		APIInterval: 200, // 默认 200ms 间隔
		ConfigName:  "download",
		limiter:     newAPILimiter(200 * time.Millisecond),
	}
}

// Init 初始化下载器
func (d *QBittorrentDownloader) Init(config *model.DownloaderConfig) error {
	if config == nil {
		return fmt.Errorf("配置不能为空")
	}

	// 保存配置
	d.config = config
	if d.limiter == nil {
		d.limiter = newAPILimiter(time.Duration(d.APIInterval) * time.Millisecond)
	} else {
		d.limiter.SetInterval(time.Duration(d.APIInterval) * time.Millisecond)
	}

	// 构建完整的 URL（添加协议前缀）
	baseURL := d.config.Host
	if !strings.HasPrefix(baseURL, "http") {
		if d.config.Ssl {
			baseURL = "https://" + baseURL
		} else {
			baseURL = "http://" + baseURL
		}
	}

	d.client.SetBaseURL(baseURL)
	d.client.SetTimeout(10 * time.Second)

	// 设置默认请求头
	d.client.SetHeaders(map[string]string{
		"User-Agent": "goto-bangumi",
	})

	// qBittorrent 5.2.0+ 的 API key 以 Bearer token 方式认证, 无状态, 无需登录
	d.client.SetAuthToken(strings.TrimSpace(d.config.Token))

	// 设置 TLS 配置
	// 如果不验证 SSL，则跳过证书验证
	if !d.config.Ssl {
		d.client.SetTLSClientConfig(&tls.Config{
			InsecureSkipVerify: true,
		})
	}
	return nil
}

func (d *QBittorrentDownloader) wait(ctx context.Context) error {
	if d.limiter == nil {
		return nil
	}
	return d.limiter.Wait(ctx, "qBittorrent")
}

// Check 检查连通性和 API key 是否可用 (需要 qBittorrent 5.2.0+)
func (d *QBittorrentDownloader) Check(ctx context.Context) error {
	if strings.TrimSpace(d.config.Token) == "" {
		return fmt.Errorf("[qBittorrent] API key 为空，请配置 token")
	}
	if err := d.wait(ctx); err != nil {
		return err
	}

	resp, err := d.client.R().SetContext(ctx).Get(QBAPI["version"])
	if err != nil {
		return httpError(ctx, "连接到qBittorrent时出错，请检查主机配置", err)
	}

	switch resp.StatusCode() {
	case 200:
		return nil
	case 401, 403:
		return fmt.Errorf("[qBittorrent] API key 无效，请检查 token 配置（需要 qBittorrent 5.2.0+）")
	}
	return fmt.Errorf("[qBittorrent] 检查失败：状态码 %d", resp.StatusCode())
}

// AddCategory 添加分类
func (d *QBittorrentDownloader) AddCategory(category string) (bool, error) {
	if err := d.wait(context.Background()); err != nil {
		return false, err
	}

	resp, err := d.client.R().
		SetFormData(map[string]string{
			"category": category,
		}).
		Post(QBAPI["createCategory"])
	if err != nil {
		return false, err
	}

	if resp.StatusCode() == 200 {
		return true, nil
	}

	return false, fmt.Errorf("添加分类失败：状态码 %d", resp.StatusCode())
}

// GetTorrentFiles 获取种子文件列表
func (d *QBittorrentDownloader) GetTorrentFiles(ctx context.Context, hash string) ([]string, error) {
	if err := d.wait(ctx); err != nil {
		return nil, err
	}

	resp, err := d.client.R().
		SetContext(ctx).
		SetQueryParam("hash", hash).
		Get(QBAPI["getFiles"])
	if err != nil {
		return nil, httpError(ctx, "qBittorrent getFiles", err)
	}

	// 404 表示种子不存在
	if resp.StatusCode() == 404 {
		slog.Warn("[qBittorrent] 找不到种子", "hash", hash)
		return nil, nil
	}

	if strings.Contains(resp.String(), "Not Found") {
		slog.Warn("[qBittorrent] 找不到种子", "hash", hash)
		return nil, nil
	}

	slog.Debug("[qBittorrent] 获取种子文件列表响应", "hash", hash, "response", resp.String())
	if resp.StatusCode() == 200 {
		var files []model.QBTorrentFile
		if err := json.Unmarshal(resp.Body(), &files); err != nil {
			return nil, fmt.Errorf("解析文件列表失败: %w", err)
		}

		fileNames := make([]string, 0, len(files))
		for _, file := range files {
			fileNames = append(fileNames, file.Name)
		}
		return fileNames, nil
	}

	return nil, fmt.Errorf("获取文件列表失败：状态码 %d", resp.StatusCode())
}

// TorrentInfo 获取单个种子详细信息
// 返回 (连接状态, 种子信息, error)
// 种子信息: 成功时返回 TorrentDownloadInfo, 失败返回 nil
func (d *QBittorrentDownloader) GetTorrentInfo(ctx context.Context, hash string) (*model.TorrentDownloadInfo, error) {
	if err := d.wait(ctx); err != nil {
		return nil, err
	}

	resp, err := d.client.R().
		SetContext(ctx).
		SetQueryParam("hash", hash).
		Get(QBAPI["properties"])
	if err != nil {
		slog.Error("[qBittorrent] torrent_info 连接错误", "error", err)
		return nil, httpError(ctx, "qBittorrent properties", err)
	}
	if resp.StatusCode() == 401 || resp.StatusCode() == 403 {
		return nil, fmt.Errorf("[qBittorrent] API key 无效，请检查 token 配置（需要 qBittorrent 5.2.0+）")
	}

	// 404 表示种子不存在但连接正常
	if resp.StatusCode() == 404 {
		slog.Warn("[qBittorrent] 种子不存在", "hash", hash)
		return nil, &apperrors.DownloadKeyError{Err: fmt.Errorf("种子不存在"), Key: hash}
	}

	if resp.StatusCode() == 200 {
		var props model.QBTorrentProperties
		if err := json.Unmarshal(resp.Body(), &props); err != nil {
			return nil, fmt.Errorf("解析种子信息失败: %w", err)
		}

		slog.Debug("[qBittorrent] 种子信息", "hash", hash, "eta", props.Eta, "save_path", props.SavePath, "completion_date", props.CompletionDate)

		// 处理 completion_date
		completionDate := 0
		if props.CompletionDate != -1 {
			completionDate = int(props.CompletionDate)
		}

		result := &model.TorrentDownloadInfo{
			ETA:       int(props.Eta),
			SavePath:  props.SavePath,
			Completed: completionDate,
		}

		return result, nil
	}

	return nil, fmt.Errorf("获取种子信息失败：状态码 %d", resp.StatusCode())
}

// TorrentsInfo 获取种子信息列表
func (d *QBittorrentDownloader) TorrentsInfo(ctx context.Context, statusFilter, category string, tag *string, limit int) ([]map[string]any, error) {
	if err := d.wait(ctx); err != nil {
		return nil, err
	}

	req := d.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"filter":   statusFilter,
			"category": category,
			"sort":     "completion_on",
			"reverse":  "true",
		})

	if tag != nil {
		req.SetQueryParam("tag", *tag)
	}

	if limit > 0 {
		req.SetQueryParam("limit", fmt.Sprintf("%d", limit))
	}

	resp, err := req.Get(QBAPI["info"])
	if err != nil {
		return nil, httpError(ctx, "qBittorrent info", err)
	}

	if resp.StatusCode() == 200 {
		var torrents []model.QBTorrentInfo
		if err := json.Unmarshal(resp.Body(), &torrents); err != nil {
			return nil, fmt.Errorf("解析种子列表失败: %w", err)
		}

		// 转换回 map[string]interface{} 以保持返回值兼容性
		result := make([]map[string]any, 0, len(torrents))
		for _, t := range torrents {
			data, _ := json.Marshal(t)
			var m map[string]any
			json.Unmarshal(data, &m)
			result = append(result, m)
		}

		return result, nil
	}

	return nil, fmt.Errorf("获取种子列表失败：状态码 %d", resp.StatusCode())
}

func (d *QBittorrentDownloader) CheckHash(ctx context.Context, hash string) (string, error) {
	info, err := d.GetTorrentInfo(ctx, hash)
	if info != nil {
		return hash, nil
	}
	return hash, err
}

// Add 添加种子
func (d *QBittorrentDownloader) Add(ctx context.Context, torrentInfo *model.TorrentInfo, savePath string) ([]string, error) {
	if err := d.wait(ctx); err != nil {
		return nil, err
	}

	// 准备基础表单数据
	data := make(map[string]string)
	data["savepath"] = savePath
	data["category"] = "GotoBangumi"
	data["paused"] = "false"
	data["autoTMM"] = "false"

	var resp *resty.Response
	var err error

	// 如果有种子文件内容，作为文件上传；否则使用磁力链接
	if len(torrentInfo.File) > 0 {
		// 上传种子文件（二进制内容）
		resp, err = d.client.R().
			SetContext(ctx).
			SetFormData(data).
			SetFileReader("torrents", "torrent.torrent", bytes.NewReader(torrentInfo.File)).
			Post(QBAPI["add"])
	} else {
		// 使用磁力链接
		data["urls"] = torrentInfo.MagnetURI
		resp, err = d.client.R().
			SetContext(ctx).
			SetFormData(data).
			Post(QBAPI["add"])
	}

	if err != nil {
		return nil, httpError(ctx, "qBittorrent add", err)
	}
	if resp.StatusCode() == 401 || resp.StatusCode() == 403 {
		return nil, fmt.Errorf("[qBittorrent] API key 无效，请检查 token 配置（需要 qBittorrent 5.2.0+）")
	}
	if resp.StatusCode() == 200 {
		respText := strings.ToLower(resp.String())
		if strings.Contains(respText, "fail") {
			slog.Debug("[qBittorrent] 添加种子失败, 种子重复", "savepath", savePath, "response", respText)
			return nil, fmt.Errorf("[qBittorrent]添加种子失败: %s", respText)
		}

		// 返回 v1 和 v2 hash 列表
		hashes := make([]string, 0, 2)
		if torrentInfo.InfoHashV1 != "" {
			hashes = append(hashes, torrentInfo.InfoHashV1)
		}
		if torrentInfo.InfoHashV2 != "" {
			// v2 hash 截取前 40 个字符
			v2Hash := torrentInfo.InfoHashV2
			if len(v2Hash) > 40 {
				v2Hash = v2Hash[:40]
			}
			hashes = append(hashes, v2Hash)
		}

		return hashes, nil
	}
	return nil, fmt.Errorf("[qBittorrent]添加种子失败：状态码 %d", resp.StatusCode())
}

// Delete 删除种子
func (d *QBittorrentDownloader) Delete(ctx context.Context, hashes []string) (bool, error) {
	if err := d.wait(ctx); err != nil {
		return false, err
	}

	hashesStr := strings.Join(hashes, "|")

	resp, err := d.client.R().
		SetContext(ctx).
		SetFormData(map[string]string{
			"hashes":      hashesStr,
			"deleteFiles": "true",
		}).
		Post(QBAPI["delete"])
	if err != nil {
		return false, httpError(ctx, "qBittorrent delete", err)
	}

	if resp.StatusCode() == 200 {
		return true, nil
	}

	return false, fmt.Errorf("删除种子失败：状态码 %d", resp.StatusCode())
}

// Rename 重命名种子文件
func (d *QBittorrentDownloader) Rename(ctx context.Context, torrentHash, oldPath, newPath string) (bool, error) {
	if err := d.wait(ctx); err != nil {
		return false, err
	}

	resp, err := d.client.R().
		SetContext(ctx).
		SetFormData(map[string]string{
			"hash":    torrentHash,
			"oldPath": oldPath,
			"newPath": newPath,
		}).
		Post(QBAPI["renameFile"])
	if err != nil {
		return false, httpError(ctx, "qBittorrent renameFile", err)
	}

	if resp.StatusCode() == 200 {
		return true, nil
	}

	// 409 表示文件已存在
	if resp.StatusCode() == 409 {
		slog.Error("[qBittorrent] 重命名错误，文件已存在", "oldPath", oldPath, "newPath", newPath)
		return false, fmt.Errorf("文件已存在")
	}

	return false, fmt.Errorf("重命名失败：状态码 %d", resp.StatusCode())
}

// Move 移动种子到新位置
func (d *QBittorrentDownloader) Move(ctx context.Context, hashes []string, newLocation string) (bool, error) {
	if err := d.wait(ctx); err != nil {
		return false, err
	}

	hashesStr := strings.Join(hashes, "|")

	resp, err := d.client.R().
		SetContext(ctx).
		SetFormData(map[string]string{
			"hashes":   hashesStr,
			"location": newLocation,
		}).
		Post(QBAPI["setLocation"])
	if err != nil {
		return false, httpError(ctx, "qBittorrent setLocation", err)
	}

	if resp.StatusCode() == 200 {
		return true, nil
	}

	return false, fmt.Errorf("移动种子失败：状态码 %d", resp.StatusCode())
}

// SetCategory 设置种子分类
func (d *QBittorrentDownloader) SetCategory(hash, category string) (bool, error) {
	if err := d.wait(context.Background()); err != nil {
		return false, err
	}

	resp, err := d.client.R().
		SetFormData(map[string]string{
			"hashes":   hash,
			"category": category,
		}).
		Post(QBAPI["setCategory"])
	if err != nil {
		return false, err
	}

	if resp.StatusCode() == 200 {
		return true, nil
	}

	return false, fmt.Errorf("设置分类失败：状态码 %d", resp.StatusCode())
}

// AddTag 添加标签
func (d *QBittorrentDownloader) AddTag(hash, tag string) (bool, error) {
	if err := d.wait(context.Background()); err != nil {
		return false, err
	}

	resp, err := d.client.R().
		SetFormData(map[string]string{
			"hashes": hash,
			"tags":   tag,
		}).
		Post(QBAPI["addTags"])
	if err != nil {
		return false, err
	}

	if resp.StatusCode() == 200 {
		return true, nil
	}

	return false, fmt.Errorf("添加标签失败：状态码 %d", resp.StatusCode())
}

// SetPreferences 设置偏好设置
func (d *QBittorrentDownloader) SetPreferences(prefs map[string]interface{}) error {
	if err := d.wait(context.Background()); err != nil {
		return err
	}

	resp, err := d.client.R().
		SetBody(prefs).
		Post(QBAPI["setPreferences"])
	if err != nil {
		return err
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("设置偏好失败：状态码 %d", resp.StatusCode())
	}

	return nil
}
