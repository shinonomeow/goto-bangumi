package downloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	clouddrive "goto-bangumi/gen"
	"goto-bangumi/internal/apperrors"
	"goto-bangumi/internal/model"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

/*
1: 登陆功能使用 api token, 用来简化登陆认证
2: 离线下载可以交多个磁力， 但是只能接收磁力， 同时能返回是否成功，失败原因（重复添加），以及返回一个路径（不知道是什么，要等会看看能不能标识）
3: check 可以直接返回，暂时没有什么要检查的，这个的返回足够了，不像qb一样，不知道有没有正常加入, 但是要想要用什么做key
4: 如何检查下载是否完成了，这个还是挺难的， 两个思路，一个是用返回的离线下载列表，这个有进度，但好用不好用就不知道了
另一个就是检查文件是否在，但是要强制刷新目录
5: 删除的话可以直接调用删除文件，但是否要把对应的磁力给删了，有待考虑
6: 重命名和移动，可以直接用api，问题不大，但是要考虑覆盖的问题,但这个可以放在上层来统一处理
7: api limit要从 get config拿到api limit
8: 怎么获取已经下载的文件是个问题，目前不知道数据大了之后会怎么样，所以后面还是要用分页来获取，但是怎么拿到要填的参数是个问题
9: 想了想还是先 下载到 savePath下面， 然后再移动到 mediaPath下面， 但是要创建新的目录
*/

// CloudDriveDownloader implements BaseDownloader using CloudDrive2 gRPC API.
// It maps torrent offline downloads onto the standard downloader interface:
//   - "hash"     = torrent info hash (same meaning as qBittorrent)
//   - "savePath" = CloudDrive2 virtual path, e.g. /115Open/Downloads
//
// CloudDrive2 endpoint is the gRPC address, default port 19798.
type CloudDriveDownloader struct {
	conn        *grpc.ClientConn
	rpc         clouddrive.CloudDriveFileSrvClient
	config      *model.DownloaderConfig
	mu          sync.RWMutex
	token       string
	cloudName   string
	cloudAcctID string
	apiInterval int
	limiter     *apiLimiter
}

func NewCloudDriveDownloader() *CloudDriveDownloader {
	return &CloudDriveDownloader{
		apiInterval: 5,
		limiter:     newAPILimiterFromQPS(5),
	}
}

// Init creates the gRPC connection and stores the API token.
func (d *CloudDriveDownloader) Init(config *model.DownloaderConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	d.config = config
	d.token = strings.TrimSpace(config.Token)
	if d.limiter == nil {
		d.limiter = newAPILimiterFromQPS(float64(d.apiInterval))
	}

	var cred grpc.DialOption
	// WARN: 并不支持使用 ssl 的连接，不知道哪里弄错了，还是用本地的吧
	if config.Ssl {
		cred = grpc.WithTransportCredentials(credentials.NewTLS(nil))
	} else {
		cred = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	conn, err := grpc.NewClient(config.Host, cred)
	if err != nil {
		return fmt.Errorf("CloudDrive2 connect to %s: %w", config.Host, err)
	}
	d.conn = conn
	d.rpc = clouddrive.NewCloudDriveFileSrvClient(conn)
	return nil
}

func (d *CloudDriveDownloader) wait(ctx context.Context) error {
	if d.limiter == nil {
		return nil
	}
	return d.limiter.Wait(ctx, "CloudDrive2")
}

// authCtx attaches the stored API token to outgoing gRPC metadata.
func (d *CloudDriveDownloader) authCtx(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+d.token)
}

// Check verifies the API token and resolves the cloud account used by SavePath
// and its API rate limit. Called once at startup.
func (d *CloudDriveDownloader) Check(ctx context.Context) error {
	if d.token == "" {
		return fmt.Errorf("[CloudDrive2] API token 为空，请配置 token")
	}
	// 发起一次请求GetAllCloudApis来获取有什么云盘， 然后从中拿到 CloudName 和 UserName
	// 然后发起一次请求GetCloudAPIConfig来拿到 maxQueriesPerSecond
	// API Limit
	aCtx := d.authCtx(ctx)
	if err := d.wait(ctx); err != nil {
		return err
	}
	apis, err := d.rpc.GetAllCloudApis(aCtx, &emptypb.Empty{})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.PermissionDenied {
			return fmt.Errorf("[CloudDrive2] get cloud APIs permission denied, please allow <Get Cloud APIs>: %w", err)
		}
		return fmt.Errorf("[CloudDrive2] could not fetch cloud APIs: %w", err)
	}
	if len(apis.GetApis()) == 0 {
		return fmt.Errorf("[CloudDrive2] no cloud account found")
	}

	// 从 SavePath 的第一个路径段提取云盘名称，匹配 API 列表来确定使用哪个云盘账号
	targetPath := cloudPathFromPath(d.config.SavePath)
	selected := apis.GetApis()[0]
	for _, api := range apis.GetApis() {
		if api.GetPath() == targetPath {
			selected = api
			break
		}
	}
	cloudName := selected.GetName()
	userName := selected.GetUserName()
	apiInterval := d.apiInterval
	if err := d.wait(ctx); err != nil {
		return err
	}
	config, err := d.rpc.GetCloudAPIConfig(aCtx, &clouddrive.GetCloudAPIConfigRequest{
		CloudName: cloudName,
		UserName:  userName,
	})
	// 能到这里就说明 权限是正常的
	if err != nil {
		slog.Warn("[CloudDrive2] could not fetch cloud API config", "cloudName", cloudName, "userName", userName, "error", err)
	} else {
		maxQueriesPerSecond := config.GetMaxQueriesPerSecond()
		if maxQueriesPerSecond > 0 {
			apiInterval = int(maxQueriesPerSecond)
			d.limiter.SetQPS(maxQueriesPerSecond)
		}
	}
	d.mu.Lock()
	d.cloudName = cloudName
	d.cloudAcctID = userName
	d.apiInterval = apiInterval
	d.mu.Unlock()
	slog.Debug("[CloudDrive2] resolved cloud", "name", cloudName, "accountId", userName, "apiInterval", apiInterval)

	return nil
}

// cloudPathFromPath extracts the first path segment: "/115Open/dir" -> "/115Open".
func cloudPathFromPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if idx := strings.IndexByte(p[1:], '/'); idx >= 0 {
		return p[:idx+1]
	}
	return p
}

// Add submits a magnet URI to CloudDrive2 offline download queue.
// Returns the info hashes already known from TorrentInfo since CloudDrive2
// does not echo them back in the response.
// 被骗了， 离线就只能返回成功和不成功！
// 用户必须保证 savePath 的存在
// 还要处理路径不存在的情况，不然会报错，挺烦的， 之前那种 qb 的方式就行不通了， 要分download path 和 media path
// 但是好像还是没有解决问题！
// 创目录还要一级一级的创！要了老命了
// 移动后还要删除空的目录
func (d *CloudDriveDownloader) Add(ctx context.Context, torrentInfo *model.TorrentInfo, savePath string) ([]string, error) {
	url := torrentInfo.MagnetURI
	if url == "" {
		return nil, fmt.Errorf("[CloudDrive2] magnet URI required for offline download")
	}

	if err := d.wait(ctx); err != nil {
		return nil, err
	}

	result, err := d.rpc.AddOfflineFiles(d.authCtx(ctx), &clouddrive.AddOfflineFileRequest{
		Urls:     url,
		ToFolder: savePath,
	})
	if err != nil {
		return nil, rpcError(ctx, "CloudDrive2 AddOfflineFiles", err)
	}
	if !result.GetSuccess() {
		return nil, fmt.Errorf("[CloudDrive2] add offline failed: %s", result.GetErrorMessage())
	}

	var hashes []string
	if torrentInfo.InfoHashV1 != "" {
		hashes = append(hashes, torrentInfo.InfoHashV1)
	}
	if torrentInfo.InfoHashV2 != "" {
		v2 := torrentInfo.InfoHashV2
		if len(v2) > 40 {
			v2 = v2[:40]
		}
		hashes = append(hashes, v2)
	}
	return hashes, nil
}

// listOfflineFiles returns all offline download tasks under SavePath.
func (d *CloudDriveDownloader) listOfflineFiles(ctx context.Context) ([]*clouddrive.OfflineFile, error) {
	if err := d.wait(ctx); err != nil {
		return nil, err
	}

	result, err := d.rpc.ListOfflineFilesByPath(d.authCtx(ctx), &clouddrive.FileRequest{
		Path: d.config.SavePath,
	})
	if err != nil {
		return nil, rpcError(ctx, "CloudDrive2 ListOfflineFilesByPath", err)
	}
	return result.GetOfflineFiles(), nil
}

// GetTorrentInfo looks up an offline download task by its info hash.
func (d *CloudDriveDownloader) GetTorrentInfo(ctx context.Context, hash string) (*model.TorrentDownloadInfo, error) {
	files, err := d.listOfflineFiles(ctx)
	if err != nil {
		return nil, err
	}

	hashLower := strings.ToLower(hash)
	for _, f := range files {
		if strings.ToLower(f.GetInfoHash()) == hashLower {
			info := &model.TorrentDownloadInfo{
				SavePath: d.config.SavePath,
				ETA:      -1,
			}
			if f.GetStatus() == clouddrive.OfflineFileStatus_OFFLINE_FINISHED {
				info.ETA = 0
				info.Completed = int(time.Now().Unix())
			}
			return info, nil
		}
	}
	return nil, &apperrors.DownloadKeyError{
		Err: fmt.Errorf("offline task not found"),
		Key: hash,
	}
}

// GetTorrentFiles lists video/subtitle files in the save folder corresponding
// to the named offline download identified by hash.
// 对于 cd2 来说， hash 应该是其下载的名字, 应该是 下载路径/hash/ 下面的文件
// 比如的 path/bangumi name/ season 1/ e
func (d *CloudDriveDownloader) GetTorrentFiles(ctx context.Context, hash string) ([]string, error) {
	offlineFiles, err := d.listOfflineFiles(ctx)
	if err != nil {
		return nil, err
	}

	hashLower := strings.ToLower(hash)
	var taskName string
	for _, f := range offlineFiles {
		if strings.ToLower(f.GetInfoHash()) == hashLower {
			taskName = f.GetName()
			break
		}
	}
	if taskName == "" {
		return nil, &apperrors.DownloadKeyError{
			Err: fmt.Errorf("offline task not found"),
			Key: hash,
		}
	}

	folderPath := d.config.SavePath + "/" + taskName
	subFiles, err := d.listSubFiles(ctx, folderPath)
	if err != nil {
		// Task may still be downloading; return no files rather than error
		slog.Warn("[CloudDrive2] GetTorrentFiles: could not list folder", "path", folderPath, "error", err)
		return nil, nil
	}

	names := make([]string, 0, len(subFiles))
	for _, f := range subFiles {
		if !f.GetIsDirectory() {
			names = append(names, taskName+"/"+f.GetName())
		}
	}
	return names, nil
}

// listSubFiles collects all file entries from the streaming GetSubFiles RPC.
func (d *CloudDriveDownloader) listSubFiles(ctx context.Context, path string) ([]*clouddrive.CloudDriveFile, error) {
	if err := d.wait(ctx); err != nil {
		return nil, err
	}

	stream, err := d.rpc.GetSubFiles(d.authCtx(ctx), &clouddrive.ListSubFileRequest{
		Path:         path,
		ForceRefresh: false,
	})
	if err != nil {
		return nil, rpcError(ctx, "CloudDrive2 GetSubFiles", err)
	}

	var all []*clouddrive.CloudDriveFile
	for {
		reply, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, rpcError(ctx, "CloudDrive2 GetSubFiles", err)
		}
		all = append(all, reply.GetSubFiles()...)
	}
	return all, nil
}

// TorrentsInfo returns all offline download tasks as generic maps.
// statusFilter, category, tag, limit are accepted for interface compatibility
// but CloudDrive2 does not support server-side filtering on offline tasks.
func (d *CloudDriveDownloader) TorrentsInfo(ctx context.Context, statusFilter, category string, tag *string, limit int) ([]map[string]any, error) {
	files, err := d.listOfflineFiles(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(files))
	for i, f := range files {
		if limit > 0 && i >= limit {
			break
		}
		entry := map[string]any{
			"hash":      f.GetInfoHash(),
			"name":      f.GetName(),
			"size":      f.GetSize(),
			"status":    f.GetStatus().String(),
			"progress":  f.GetPercendDone(),
			"save_path": d.config.SavePath,
			"category":  "GotoBangumi",
		}
		result = append(result, entry)
	}
	return result, nil
}

// CheckHash verifies an info hash exists in the offline download list.
func (d *CloudDriveDownloader) CheckHash(ctx context.Context, hash string) (string, error) {
	files, err := d.listOfflineFiles(ctx)
	if err != nil {
		return hash, err
	}
	hashLower := strings.ToLower(hash)
	for _, f := range files {
		if strings.ToLower(f.GetInfoHash()) == hashLower {
			return hash, nil
		}
	}
	return hash, &apperrors.DownloadKeyError{Err: fmt.Errorf("hash not found"), Key: hash}
}

// Rename renames a file at oldPath to newPath on the CloudDrive2 filesystem.
// torrentHash is unused; oldPath and newPath must be full CloudDrive2 virtual paths.
// 需要的是旧的完整路径， 新的文件名（不带路径）， 以及torrentHash（目前没用到， 但是接口需要）
// 传入的 newPath 是完整的，所以还要提出新的文件名
func (d *CloudDriveDownloader) Rename(ctx context.Context, torrentHash, oldPath, newPath string) (bool, error) {
	// newPath should be just the new filename, not the full path
	newName := newPath
	if idx := strings.LastIndexByte(newPath, '/'); idx >= 0 {
		newName = newPath[idx+1:]
	}

	if err := d.wait(ctx); err != nil {
		return false, err
	}

	result, err := d.rpc.RenameFile(d.authCtx(ctx), &clouddrive.RenameFileRequest{
		TheFilePath: oldPath,
		NewName:     newName,
	})
	if err != nil {
		return false, rpcError(ctx, "CloudDrive2 RenameFile", err)
	}
	if !result.GetSuccess() {
		return false, fmt.Errorf("[CloudDrive2] rename failed: %s", result.GetErrorMessage())
	}
	return true, nil
}

// Move moves files (identified by their CloudDrive2 paths) to newLocation.
func (d *CloudDriveDownloader) Move(ctx context.Context, hashes []string, newLocation string) (bool, error) {
	if err := d.wait(ctx); err != nil {
		return false, err
	}

	result, err := d.rpc.MoveFile(d.authCtx(ctx), &clouddrive.MoveFileRequest{
		TheFilePaths: hashes,
		DestPath:     newLocation,
	})
	if err != nil {
		return false, rpcError(ctx, "CloudDrive2 MoveFile", err)
	}
	if !result.GetSuccess() {
		return false, fmt.Errorf("[CloudDrive2] move failed: %s", result.GetErrorMessage())
	}
	return true, nil
}

// Delete removes offline download tasks by their info hashes.
func (d *CloudDriveDownloader) Delete(ctx context.Context, hashes []string) (bool, error) {
	// 只是无法删除文件，到也是小问题
	d.mu.RLock()
	cloudName := d.cloudName
	cloudAcctID := d.cloudAcctID
	d.mu.RUnlock()
	if cloudName == "" || cloudAcctID == "" {
		return false, fmt.Errorf("cloud account info not available")
	}

	if err := d.wait(ctx); err != nil {
		return false, err
	}

	result, err := d.rpc.RemoveOfflineFiles(d.authCtx(ctx), &clouddrive.RemoveOfflineFilesRequest{
		CloudName:      cloudName,
		CloudAccountId: cloudAcctID,
		InfoHashes:     hashes,
		DeleteFiles:    true,
	})
	if err != nil {
		return false, rpcError(ctx, "CloudDrive2 RemoveOfflineFiles", err)
	}
	if !result.GetSuccess() {
		return false, fmt.Errorf("[CloudDrive2] delete failed: %s", result.GetErrorMessage())
	}
	return true, nil
}

// GetDownloadURL returns the HTTP download URL for a file at path.
// The caller is responsible for making the HTTP request with the returned headers.
func (d *CloudDriveDownloader) GetDownloadURL(ctx context.Context, filePath string, preview bool) (string, map[string]string, error) {
	if err := d.wait(ctx); err != nil {
		return "", nil, err
	}

	info, err := d.rpc.GetDownloadUrlPath(d.authCtx(ctx), &clouddrive.GetDownloadUrlPathRequest{
		Path:         filePath,
		Preview:      preview,
		GetDirectUrl: true,
	})
	if err != nil {
		return "", nil, rpcError(ctx, "CloudDrive2 GetDownloadUrlPath", err)
	}

	// Prefer direct URL from cloud storage provider
	if directURL := info.GetDirectUrl(); directURL != "" {
		headers := info.GetAdditionalHeaders()
		if ua := info.GetUserAgent(); ua != "" {
			if headers == nil {
				headers = make(map[string]string)
			}
			headers["User-Agent"] = ua
		}
		return directURL, headers, nil
	}

	// Assemble URL from template: replace {SCHEME}, {HOST}, {PREVIEW}
	scheme := "http"
	if d.config.Ssl {
		scheme = "https"
	}
	host := d.config.Host
	previewStr := "false"
	if preview {
		previewStr = "true"
	}

	urlPath := info.GetDownloadUrlPath()
	urlPath = strings.ReplaceAll(urlPath, "{SCHEME}", scheme)
	urlPath = strings.ReplaceAll(urlPath, "{HOST}", host)
	urlPath = strings.ReplaceAll(urlPath, "{PREVIEW}", previewStr)

	assembledURL := scheme + "://" + host + urlPath
	return assembledURL, nil, nil
}
