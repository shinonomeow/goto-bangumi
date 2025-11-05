package database

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"goto-bangumi/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 数据库连接包装
type DB struct {
	*gorm.DB
}

// 全局数据库实例（单例模式）
var globalDB *DB

// 用于防止并发创建相同 Bangumi 的互斥锁
var bangumiCreateMutex sync.Mutex

// NewDB 创建数据库连接
func NewDB(dsn *string) (*DB, error) {
	// 打开数据库连接，使用简单配置
	var path string = filepath.Join(".", "data", "data.db")
	if dsn != nil {
		path = *dsn
	}

	// 确保数据库目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}

	gormDB, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	// 自动迁移模型
	// 注意：迁移顺序很重要，基础表（无外键依赖）应该先迁移
	// 1. 首先迁移独立的基础表
	// 2. 然后迁移有外键关联的表
	// 3. GORM 会自动创建多对多关系的中间表（如 bangumi_parser_mappings）
	if err := gormDB.AutoMigrate(
		// 基础表（无外键依赖）
		&model.MikanItem{},
		&model.TmdbItem{},
		&model.EpisodeMetadata{},
		&model.RSSItem{},

		// 有外键依赖的表
		&model.Bangumi{}, // 依赖 MikanItem, TmdbItem，多对多关联 BangumiParse
		&model.Torrent{}, // 依赖 Bangumi, BangumiParse
	); err != nil {
		fmt.Println("Error migrating database:", err)
		return nil, err
	}

	return &DB{DB: gormDB}, nil
}

// Close 关闭数据库连接
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// ============ 单例模式相关方法 ============

// InitDB 初始化全局数据库实例
func InitDB(dsn *string) error {
	if globalDB != nil {
		return nil // 已经初始化，直接返回
	}
	db, err := NewDB(dsn)
	if err != nil {
		return err
	}
	globalDB = db
	return nil
}

// GetDB 获取全局数据库实例
func GetDB() *DB {
	if globalDB == nil {
		InitDB(nil)
	}

	return globalDB
}

// CloseDB 关闭全局数据库连接
func CloseDB() error {
	if globalDB != nil {
		err := globalDB.Close()
		globalDB = nil
		return err
	}
	return nil
}

// ============ Bangumi 相关方法 ============

// CreateBangumi 创建番剧
func (db *DB) CreateBangumi(bangumi *model.Bangumi) error {
	// 加锁防止并发创建重复的 Bangumi
	bangumiCreateMutex.Lock()
	defer bangumiCreateMutex.Unlock()

	// 对于 Bangumi 要进行一个查重, 主要是看其对应的 mikanid 和 tmdbid
	// 先是看 mikanid 有的话
	var oldBangumi model.Bangumi
	var tmdbID int
	if bangumi.TmdbID != nil {
		tmdbID = *bangumi.TmdbID
	} else if bangumi.TmdbItem != nil {
		tmdbID = bangumi.TmdbItem.ID
	}
	var mikanID int
	if bangumi.MikanID != nil {
		mikanID = *bangumi.MikanID
	} else if bangumi.MikanItem != nil {
		mikanID = bangumi.MikanItem.ID
	}
	// 通过 mikanID 和 tmdbID 来查找 Bangumi
	// err := db.Where("mikan_id = ? AND tmdb_id = ?", mikanID, tmdbID).First(&oldBangumi).Error
	err := db.Preload("MikanItem").
		Preload("TmdbItem").
		Preload("EpisodeMetadata").
		Where("mikan_id = ?", mikanID).
		Or("tmdb_id", tmdbID).First(&oldBangumi).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if oldBangumi.ID != 0 {
		// 找到的话就更新一下 mikan, tmdb
		slog.Debug("番剧已存在，进行更新", slog.String("标题", oldBangumi.OfficialTitle))
		if oldBangumi.MikanID == nil && bangumi.MikanItem != nil {
			oldBangumi.MikanItem = bangumi.MikanItem
		}
		if oldBangumi.TmdbID == nil && bangumi.TmdbItem != nil {
			oldBangumi.TmdbItem = bangumi.TmdbItem
		}
		// 修复：只添加不存在的 EpisodeMetadata，避免重复
		for _, newMeta := range bangumi.EpisodeMetadata {
			exists := false
			for _, existingMeta := range oldBangumi.EpisodeMetadata {
				// 通过 Title、Season、Group、Resolution 判断是否为同一条记录
				if existingMeta.Title == newMeta.Title &&
					existingMeta.Season == newMeta.Season &&
					existingMeta.Group == newMeta.Group &&
					existingMeta.Resolution == newMeta.Resolution &&
					existingMeta.SubType == newMeta.SubType {
					exists = true
					break
				}
			}
			if !exists {
				newMeta.BangumiID = oldBangumi.ID
				oldBangumi.EpisodeMetadata = append(oldBangumi.EpisodeMetadata, newMeta)
			}
		}
		return db.Save(&oldBangumi).Error
	}
	return db.Save(bangumi).Error
}

// UpdateBangumi 更新番剧
func (db *DB) UpdateBangumi(bangumi *model.Bangumi) error {
	return db.Save(bangumi).Error
}

// DeleteBangumi 删除番剧
func (db *DB) DeleteBangumi(id int) error {
	return db.Delete(&model.Bangumi{}, id).Error
}

// GetBangumiByID 根据 ID 获取番剧
func (db *DB) GetBangumiByID(id int) (*model.Bangumi, error) {
	var bangumi model.Bangumi
	err := db.First(&bangumi, id).Error
	if err != nil {
		return nil, err
	}
	return &bangumi, nil
}

// ListBangumi 获取所有番剧
func (db *DB) ListBangumi() ([]*model.Bangumi, error) {
	var bangumis []*model.Bangumi
	err := db.Find(&bangumis).Error
	return bangumis, err
}

// ============ RSS 相关方法 ============

// CreateRSS 创建 RSS 项
func (db *DB) CreateRSS(item *model.RSSItem) error {
	return db.Save(item).Error
}

// UpdateRSS 更新 RSS 项
func (db *DB) UpdateRSS(item *model.RSSItem) error {
	return db.Save(item).Error
}

// DeleteRSS 删除 RSS 项
func (db *DB) DeleteRSS(id uint) error {
	return db.Delete(&model.RSSItem{}, id).Error
}

// GetRSSByID 根据 ID 获取 RSS 项
func (db *DB) GetRSSByID(id uint) (*model.RSSItem, error) {
	var item model.RSSItem
	err := db.First(&item, id).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// GetRSSByURL 根据 URL 获取 RSS 项
func (db *DB) GetRSSByURL(url string) (*model.RSSItem, error) {
	var item model.RSSItem
	err := db.Where("url = ?", url).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ListRSS 获取所有 RSS 项
func (db *DB) ListRSS() ([]*model.RSSItem, error) {
	var items []*model.RSSItem
	err := db.Find(&items).Error
	return items, err
}

// ListActiveRSS 获取所有激活的 RSS 项
func (db *DB) ListActiveRSS() ([]*model.RSSItem, error) {
	var items []*model.RSSItem
	err := db.Where("enabled = ?", true).Find(&items).Error
	return items, err
}

// SetRSSEnabled 设置 RSS 项的启用状态
func (db *DB) SetRSSEnabled(id uint, enabled bool) error {
	return db.Model(&model.RSSItem{}).
		Where("id = ?", id).
		Update("enabled", enabled).Error
}

// ============ Torrent 相关方法 ============

// CreateTorrent 创建或更新种子
func (db *DB) CreateTorrent(torrent *model.Torrent) error {
	return db.Save(torrent).Error
}

// UpdateTorrent 更新种子
func (db *DB) UpdateTorrent(torrent *model.Torrent) error {
	return db.Save(torrent).Error
}

// DeleteTorrent 删除种子
func (db *DB) DeleteTorrent(id uint) error {
	return db.Delete(&model.Torrent{}, id).Error
}

// GetTorrentByID 根据 ID 获取种子
func (db *DB) GetTorrentByID(id uint) (*model.Torrent, error) {
	var torrent model.Torrent
	err := db.First(&torrent, id).Error
	if err != nil {
		return nil, err
	}
	return &torrent, nil
}

// GetTorrentByURL 根据 URL 获取种子
func (db *DB) GetTorrentByURL(url string) (*model.Torrent, error) {
	var torrent model.Torrent
	err := db.Where("url = ?", url).First(&torrent).Error
	if err != nil {
		return nil, err
	}
	return &torrent, nil
}

// GetTorrentByDownloadUID 根据下载 UID 获取种子
func (db *DB) GetTorrentByDownloadUID(duid string) (*model.Torrent, error) {
	var torrent model.Torrent
	err := db.Where("download_uid = ?", duid).First(&torrent).Error
	if err != nil {
		return nil, err
	}
	return &torrent, nil
}

// ListTorrent 获取所有种子
func (db *DB) ListTorrent() ([]*model.Torrent, error) {
	var torrents []*model.Torrent
	err := db.Find(&torrents).Error
	return torrents, err
}

// ListTorrentByBangumi 根据番剧信息获取种子列表
func (db *DB) ListTorrentByBangumi(title string, season int, rssLink string) ([]*model.Torrent, error) {
	var torrents []*model.Torrent
	err := db.Where("bangumi_official_title = ? AND bangumi_season = ? AND rss_link = ?",
		title, season, rssLink).Find(&torrents).Error
	return torrents, err
}

// FindUnrenamedTorrent 查询已下载但未重命名的种子
func (db *DB) FindUnrenamedTorrent() ([]*model.Torrent, error) {
	var torrents []*model.Torrent
	err := db.Where("downloaded = ? AND renamed = ?", true, false).
		Find(&torrents).Error
	return torrents, err
}

// CheckNewTorrents 检查新种子（未下载或不存在的种子）
func (db *DB) CheckNewTorrents(torrents []*model.Torrent) ([]*model.Torrent, error) {
	var newTorrents []*model.Torrent

	for _, torrent := range torrents {
		existing, err := db.GetTorrentByURL(torrent.URL)
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, err
		}

		// 不存在或未下载的种子
		if existing == nil || !existing.Downloaded {
			newTorrents = append(newTorrents, torrent)
		}
	}

	return newTorrents, nil
}

// DeleteTorrentByURL 根据 URL 删除种子
func (db *DB) DeleteTorrentByURL(url string) error {
	return db.Where("url = ?", url).Delete(&model.Torrent{}).Error
}

// DeleteTorrentByDownloadUID 根据下载 UID 删除种子
func (db *DB) DeleteTorrentByDownloadUID(duid string) error {
	return db.Where("download_uid = ?", duid).Delete(&model.Torrent{}).Error
}

// ============ Mikan 关联方法 ============

// CreateMikanItem 创建或更新 Mikan 项
func (db *DB) CreateMikanItem(item *model.MikanItem) error {
	return db.Save(item).Error
}

// GetMikanItemByID 根据 MikanID 获取 Mikan 项
func (db *DB) GetMikanItemByID(mikanID int) (*model.MikanItem, error) {
	var item model.MikanItem
	err := db.Where("mikan_id = ?", mikanID).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// GetBangumisByMikanID 根据 MikanID 查找所有关联的 Bangumi
func (db *DB) GetBangumisByMikanID(mikanID int) ([]*model.Bangumi, error) {
	var bangumis []*model.Bangumi
	err := db.Where("mikan_id = ?", mikanID).Find(&bangumis).Error
	return bangumis, err
}

// UpdateBangumiMikan 更新 Bangumi 的 Mikan 关联
func (db *DB) UpdateBangumiMikan(bangumiID uint, mikanID int) error {
	return db.Model(&model.Bangumi{}).
		Where("id = ?", bangumiID).
		Update("mikan_id", mikanID).Error
}

// RemoveBangumiMikan 移除 Bangumi 的 Mikan 关联
func (db *DB) RemoveBangumiMikan(bangumiID uint) error {
	return db.Model(&model.Bangumi{}).
		Where("id = ?", bangumiID).
		Update("mikan_id", nil).Error
}

// ============ TMDB 关联方法 ============

// CreateTmdbItem 创建或更新 TMDB 项
func (db *DB) CreateTmdbItem(item *model.TmdbItem) error {
	return db.Save(item).Error
}

// GetTmdbItemByID 根据 TmdbID 获取 TMDB 项
func (db *DB) GetTmdbItemByID(tmdbID int) (*model.TmdbItem, error) {
	var item model.TmdbItem
	err := db.Where("tmdb_id = ?", tmdbID).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// GetBangumisByTmdbID 根据 TmdbID 查找所有关联的 Bangumi
func (db *DB) GetBangumisByTmdbID(tmdbID int) ([]*model.Bangumi, error) {
	var bangumis []*model.Bangumi
	err := db.Where("tmdb_id = ?", tmdbID).Find(&bangumis).Error
	return bangumis, err
}

// UpdateBangumiTmdb 更新 Bangumi 的 TMDB 关联
func (db *DB) UpdateBangumiTmdb(bangumiID uint, tmdbID int) error {
	return db.Model(&model.Bangumi{}).
		Where("id = ?", bangumiID).
		Update("tmdb_id", tmdbID).Error
}

// RemoveBangumiTmdb 移除 Bangumi 的 TMDB 关联
func (db *DB) RemoveBangumiTmdb(bangumiID uint) error {
	return db.Model(&model.Bangumi{}).
		Where("id = ?", bangumiID).
		Update("tmdb_id", nil).Error
}

// ============ BangumiParse 关联方法 ============

// CreateBangumiParse 创建番剧解析器
func (db *DB) CreateBangumiParse(parser *model.EpisodeMetadata) error {
	return db.Save(parser).Error
}

func (db *DB) GetBangumiParseByTitle(torrentName string) (*model.EpisodeMetadata, error) {
	// 要求 Title 和 Group 都在 torrentName 中出现
	// title 和 group 是 torrentName 的子串
	var parser model.EpisodeMetadata
	err := db.Where("instr(?, title) > 0 AND instr(?, `group`) > 0", torrentName, torrentName).First(&parser).Error
	if err != nil {
		return nil, err
	}
	return &parser, nil
}

// GetBangumiParseByID 根据 ID 获取番剧解析器
func (db *DB) GetBangumiParseByID(id uint) (*model.EpisodeMetadata, error) {
	var parser model.EpisodeMetadata
	err := db.First(&parser, id).Error
	if err != nil {
		return nil, err
	}
	return &parser, nil
}

// GetBangumisByParseID 根据 ParseID 查找所有关联的 Bangumi
func (db *DB) GetBangumisByParseID(parserID uint) ([]*model.Bangumi, error) {
	var bangumis []*model.Bangumi
	err := db.Joins("JOIN bangumi_parser_mappings ON bangumi.id = bangumi_parser_mappings.bangumi_id").
		Where("bangumi_parser_mappings.bangumi_parser_id = ?", parserID).
		Find(&bangumis).Error
	return bangumis, err
}

// GetParsesByBangumiID 根据 BangumiID 查找所有关联的 Parse
func (db *DB) GetParsesByBangumiID(bangumiID uint) ([]*model.EpisodeMetadata, error) {
	var parsers []*model.EpisodeMetadata
	err := db.Joins("JOIN bangumi_parser_mappings ON bangumi_parser.id = bangumi_parser_mappings.bangumi_parser_id").
		Where("bangumi_parser_mappings.bangumi_id = ?", bangumiID).
		Find(&parsers).Error
	return parsers, err
}

// ============ Bangumi 复合查询方法 ============

// GetBangumiWithDetails 获取 Bangumi 及其关联的 TMDB、Mikan、Parse 信息
func (db *DB) GetBangumiWithDetails(id uint) (*model.Bangumi, error) {
	var bangumi model.Bangumi
	err := db.Preload("TmdbItem").
		Preload("MikanItem").
		Preload("EpisodeMetadata").
		First(&bangumi, id).Error
	if err != nil {
		return nil, err
	}
	return &bangumi, nil
}

// ListBangumiWithDetails 获取所有 Bangumi 及其关联信息
func (db *DB) ListBangumiWithDetails() ([]*model.Bangumi, error) {
	var bangumis []*model.Bangumi
	err := db.Preload("TmdbItem").
		Preload("MikanItem").
		Preload("EpisodeMetadata").
		Find(&bangumis).Error
	return bangumis, err
}

// ============ Bangumi 和 BangumiParse 多对多关联方法 ============

// AddParsesToBangumi 为 Bangumi 添加多个 Parse（一对多关系）
func (db *DB) AddParsesToBangumi(bangumiID int, parsers []*model.EpisodeMetadata) error {
	var bangumi model.Bangumi
	if err := db.First(&bangumi, bangumiID).Error; err != nil {
		return err
	}
	return db.Model(&bangumi).Association("EpisodeMetadata").Append(parsers)
}

// ReplaceParsesToBangumi 替换 Bangumi 的所有 Parse（一对多关系）
func (db *DB) ReplaceParsesToBangumi(bangumiID int, parsers []*model.EpisodeMetadata) error {
	var bangumi model.Bangumi
	if err := db.First(&bangumi, bangumiID).Error; err != nil {
		return err
	}
	return db.Model(&bangumi).Association("EpisodeMetadata").Replace(parsers)
}

// RemoveParsesFromBangumi 从 Bangumi 中移除指定的 Parse（一对多关系）
func (db *DB) RemoveParsesFromBangumi(bangumiID int, parsers []*model.EpisodeMetadata) error {
	var bangumi model.Bangumi
	if err := db.First(&bangumi, bangumiID).Error; err != nil {
		return err
	}
	return db.Model(&bangumi).Association("EpisodeMetadata").Delete(parsers)
}

// ClearParsesFromBangumi 清空 Bangumi 的所有 Parse（一对多关系）
func (db *DB) ClearParsesFromBangumi(bangumiID int) error {
	var bangumi model.Bangumi
	if err := db.First(&bangumi, bangumiID).Error; err != nil {
		return err
	}
	return db.Model(&bangumi).Association("EpisodeMetadata").Clear()
}

// CountParsesOfBangumi 统计 Bangumi 关联的 Parse 数量
func (db *DB) CountParsesOfBangumi(bangumiID int) (int64, error) {
	var bangumi model.Bangumi
	if err := db.First(&bangumi, bangumiID).Error; err != nil {
		return 0, err
	}
	return db.Model(&bangumi).Association("EpisodeMetadata").Count(), nil
}

// AddBangumiToParse 为 Parse 添加 Bangumi（多对多关系反向操作）
func (db *DB) AddBangumiToParse(parserID int, bangumis []*model.Bangumi) error {
	var parser model.EpisodeMetadata
	if err := db.First(&parser, parserID).Error; err != nil {
		return err
	}
	return db.Model(&parser).Association("Bangumis").Append(bangumis)
}

// ============ Torrent 关联查询优化方法 ============

// GetTorrentWithDetails 获取 Torrent 及其关联的 Bangumi 和 Parse 信息
func (db *DB) GetTorrentWithDetails(url string) (*model.Torrent, error) {
	var torrent model.Torrent
	err := db.Preload("Bangumi").
		Preload("BangumiParse").
		Where("url = ?", url).
		First(&torrent).Error
	if err != nil {
		return nil, err
	}
	return &torrent, nil
}

// ListTorrentWithDetails 获取所有 Torrent 及其关联信息
func (db *DB) ListTorrentWithDetails() ([]*model.Torrent, error) {
	var torrents []*model.Torrent
	err := db.Preload("Bangumi").
		Preload("BangumiParse").
		Find(&torrents).Error
	return torrents, err
}
