package refresh

import (
	"context"
	"log/slog"

	"goto-bangumi/internal/database"
	"goto-bangumi/internal/download"
	"goto-bangumi/internal/model"
	"goto-bangumi/internal/network"
	"goto-bangumi/internal/parser/baseparser"
)

// 流程为: 取种子列表 -> 对比数据库中已有的种子 -> 返回新增的种子 -> 检查是否有对应的番剧信息
// -> 如果有 调用 filter, 反回符合条件的种子
// -> 如果没有, 先过一下基础 filter, 然后调用 解析

func getTorrents(url string) []*model.Torrent {
	client := network.NewRequestClient()
	torrents, _ := client.GetTorrents(url)
	db := database.GetDB()
	newTorrents, _ := db.CheckNewTorrents(torrents)
	return newTorrents
}

func pullRss(url string) []*model.Torrent {
	torrents := getTorrents(url)
	for _, t := range torrents {
		t.Bangumi.RRSSLink = url
	}
	return torrents
}

// FindNewBangumi 从 rss 里面看看没有没新的番剧
func FindNewBangumi(url string) {
	netClient := network.NewRequestClient()
	torrents, _ := netClient.GetTorrents(url)
	db := database.GetDB()
	newTorrents := make(map[string]*model.Torrent, 10)
	for _, t := range torrents {
		// 突然想起来, possess title 后,名字会和 torrent 里面的差很多,这时就会导致不停的创建
		// 这就是之前 AB 会导致不停的创建的原因, 新在已经解决了
		// 解决方案是对 torrent name 在 get 的时候就处理名字
		_, err := db.GetBangumiParseByTitle(t.Name)
		// 没有找到, 说明是新的番剧
		// 先过一下基础 filter
		if err != nil && FilterTorrent(t, nil) {
			// 要进行一个去重, 一些torrent 是没必要都解析的
			// 进行 metaparser 解析
			raw := baseparser.NewTitleMetaParse().Parse(t.Name)
			if raw != nil {
				newTorrents[raw.Title] = t
			}
		}
	}
	slog.Debug("有新番剧", "数量", len(newTorrents))
	// 将种子进行解析
	for _, t := range newTorrents {
		go createBangumi(t, url)
	}
}

func RefreshRSS(ctx context.Context, url string) {
	torrents := pullRss(url)
	db := database.GetDB()
	for _, t := range torrents {
		metaData, err := db.GetBangumiParseByTitle(t.Name)
		if err != nil {
			// 如果找不到对应的 Bangumi，跳过该种子，等待后续解析
			slog.Warn("找不到对应的番剧信息，跳过该种子", slog.String("torrent", t.Name), slog.String("error", err.Error()))
			continue
		}
		t.BangumiID = metaData.BangumiID

		// 检查该 torrent 是否已存在
		existingTorrent, _ := db.GetTorrentByURL(t.URL)
		if existingTorrent != nil && existingTorrent.Downloaded {
			slog.Debug("种子已存在且已下载，跳过", slog.String("url", t.URL))
			continue
		}

		db.CreateTorrent(t)
		download.DQueue.Add(ctx, t)
	}
}
