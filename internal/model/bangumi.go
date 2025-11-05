package model

import (
	"fmt"
	"strconv"
)

type MikanItem struct {
	ID            int    `json:"mikan_id" gorm:"primaryKey;comment:'Mikan ID'"`
	OfficialTitle string `json:"official_title" gorm:"default:'';comment:'番剧中文名'"`
	Season        int    `json:"season" gorm:"default:1;comment:'季度'"`
	PosterLink    string `json:"poster_link" gorm:"default:'';comment:'海报链接'"`
}

func (m MikanItem) String() string {
	return fmt.Sprintf(
		`MikanID: %d,
 OfficialTitle: %s,
 Season: %d,
 PosterLink: %s`, m.ID, m.OfficialTitle, m.Season, m.PosterLink)
}

func NewMikanItem() *MikanItem {
	return &MikanItem{
		Season: 1,
	}
}

type TmdbItem struct {
	ID            int     `json:"tmdb_id" gorm:"primaryKey;comment:'TMDB ID'"`
	Year          string  `json:"year" gorm:"default:'';comment:'番剧年份'"`
	OriginalTitle string  `json:"original_title" gorm:"default:'';comment:'番剧原名'"`
	AirDate       string  `json:"air_date" gorm:"default:'';comment:'首播日期'"`
	EpisodeCount  int     `json:"episode_count" gorm:"default:0;comment:'总集数'"`
	Title         string  `json:"title" gorm:"default:'';comment:'番剧名称'"`
	Season        int     `json:"season" gorm:"default:1;comment:'季度'"`
	PosterLink    string  `json:"poster_url" gorm:"default:'';comment:'海报链接'"`
	VoteAverage   float64 `json:"vote_average" gorm:"default:0;comment:'评分'"`
}

func (t TmdbItem) String() string {
	return fmt.Sprintf(
		`TmdbID: %d,
 Title: %s,
 Year: %s,
 OriginalTitle: %s,
 AirDate: %s,
 EpisodeCount: %d,
 Season: %d,
 PosterLink: %s,
 VoteAverage: %.2f`, t.ID, t.Title, t.Year, t.OriginalTitle, t.AirDate, t.EpisodeCount, t.Season, t.PosterLink, t.VoteAverage)
}

func NewTmdbItem() *TmdbItem {
	return &TmdbItem{
		Season: 1,
	}
}

// EpisodeMetadata 用来存储番剧解析器的原始信息
// 是否要认为一个 EpisodeMetadata 可以对应多个 Bangumi?
// 复合唯一索引：BangumiID + Title + Season + Group + Resolution + SubType，防止重复添加
type EpisodeMetadata struct {
	ID         int    `gorm:"primaryKey;autoIncrement"`
	Title      string `gorm:"default:'';comment:'番剧名称';uniqueIndex:idx_episode_metadata_unique"`
	Season     int    `gorm:"default:1;comment:'季度';uniqueIndex:idx_episode_metadata_unique"`
	SeasonRaw  string `gorm:"default:'';comment:'季度原名'"`
	Episode    int    `gorm:"-;comment:'集数'"`
	Sub        string `gorm:"default:'';comment:'字幕语言'"`
	SubType    string `gorm:"default:'';comment:'字幕类型';uniqueIndex:idx_episode_metadata_unique"`
	Group      string `gorm:"default:'';comment:'字幕组';uniqueIndex:idx_episode_metadata_unique"`
	Year       string `gorm:"-;comment:'年份'"`
	Resolution string `gorm:"default:'';comment:'分辨率';uniqueIndex:idx_episode_metadata_unique"`
	Source     string `gorm:"default:'';comment:'来源'"`
	AudioInfo  string `gorm:"default:'';comment:'音频信息'"`
	VideoInfo  string `gorm:"default:'';comment:'视频信息'"`
	BangumiID  int    `gorm:"index;comment:'关联的Bangumi ID';uniqueIndex:idx_episode_metadata_unique"`
}

// String 式化输出
func (e EpisodeMetadata) String() string {
	return "Title: " + e.Title +
		", Season: " + strconv.Itoa(e.Season) +
		", Episode: " + strconv.Itoa(e.Episode) +
		", Sub: " + e.Sub +
		", Group: " + e.Group +
		", Year: " + e.Year +
		", Resolution: " + e.Resolution +
		", Source: " + e.Source +
		", AudioInfo: " + e.AudioInfo +
		", VideoInfo: " + e.VideoInfo
}

// Bangumi 用于存储一些可配置的番剧信息
type Bangumi struct {
	ID            int    `gorm:"primaryKey"`
	OfficialTitle string `json:"official_title" gorm:"default:'';comment:'番剧中文名'"`
	Year          string `json:"year" gorm:"default:'';comment:'番剧年份'"`
	Season        int    `json:"season" gorm:"default:1;comment:'番剧季度'"`

	// 外键关联（一对多关系）
	MikanID *int `json:"mikan_id" gorm:"index;comment:'关联的Mikan ID'"`
	TmdbID  *int `json:"tmdb_id" gorm:"index;comment:'关联的TMDB ID'"`

	// GORM 关联对象（用于预加载）
	// 一个 Bangumi 属于一个 MikanItem 和一个 TmdbItem
	// 使用指针类型表示"可能没有"，foreignKey 指向 Bangumi 的外键字段，references 指向关联表的主键字段
	MikanItem *MikanItem `gorm:"foreignKey:MikanID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL"`
	TmdbItem  *TmdbItem  `gorm:"foreignKey:TmdbID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL"`
	// 属于一个 RSSItem
	RRSSLink string `json:"rss_link" gorm:"default:'';comment:'关联的RSS订阅链接'"`

	// has many关系，关联 BangumiParse
	EpisodeMetadata []EpisodeMetadata `gorm:"foreignKey:BangumiID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`

	EpsCollect    bool   `json:"eps_collect" gorm:"default:false;comment:'是否已收集'"`
	Offset        int    `json:"offset" gorm:"default:0;comment:'番剧偏移量'"`
	IncludeFilter string `json:"include_filter" gorm:"default:'';comment:'番剧包含过滤器'"`
	ExcludeFilter string `json:"exclude_filter" gorm:"default:'';comment:'番剧排除过滤器'"`
	Parse         string `json:"parser" gorm:"default:'tmdb';comment:'番剧解析器'"`
	PosterLink    string `json:"poster_link" gorm:"default:'';comment:'番剧海报链接'"`
	Deleted       bool   `json:"deleted" gorm:"default:false;comment:'是否已删除'"`
}

// NewBangumi 创建一个默认的 Bangumi 实例
func NewBangumi() *Bangumi {
	return &Bangumi{
		Season: 1,
		Parse:  "tmdb",
	}
}

// 重新设计几个表来确定 bangumi 和 mikanid, tmdbid , bangumiid 的关系
// mikanid -> id, mikan_id,rss_link
// tmdbid -> id, tmdb_id,rss_link, 这个 id 要加个 #season
// bangumiid -> id, bangumi_id,rss_link
// 当解析到相同的, ID, 重建 ID
// bangumi 里面要保留的项: 前端要: title,year, seasion , group,  group_name,poster_link,parser,

// 流程如下:
// 1. 解析 rss, 先排除已经下载的 torrent , 这是通过 torrent 表来做的
// 2. 对于没有下载的 torrent, 通过 BangumiParse 的 Title 得到对应的 BangumiParse ID
// 3. 通过 BangumiParseMapping 得到对应的 BangumiUID
// 4. 通过 BangumiUID 得到对应的 BangumiInfo
// 5. 通过 BangumiInfo 对 Bangumi 表进行更新/创建

// 要是没有对应的 BangumiParse, 调用 raw_parser 解析, 得到 对应的 BangumiParse, 再通过 MikanParse(如果有 homepage) 解析得到 mikan_id, 通过 mikan_id map 去找 BangumiUID
// 要是没找到 再去调用 tmdb_parser 解析, 得到 tmdb_id, 通过 tmdb_id map 去找 BangumiUID
// 对于找到了 BangumiUID 的, 更新 BangumiParseMapping, 其中要是没找到 mikan_id 的更新 mikan_id,
// 没找到 tmdb_id 的就是新的番剧了, 更新 tmdb_id, mikan_id,  BangumiInfo和对应的 mapping
// 对于 tmdb ,我们要拿到所有的集数, 用以显示下载了多少集, 还差多少集
