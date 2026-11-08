package core

import (
	"context"
	"log/slog"

	"goto-bangumi/internal/conf"
	"goto-bangumi/internal/database"
	"goto-bangumi/internal/download"
	"goto-bangumi/internal/logger"
	"goto-bangumi/internal/model"
	"goto-bangumi/internal/network"
	"goto-bangumi/internal/notification"
	"goto-bangumi/internal/parser"
	"goto-bangumi/internal/refresh"
	"goto-bangumi/internal/rename"
	"goto-bangumi/internal/scheduler"
	"goto-bangumi/internal/task"
	"goto-bangumi/internal/taskrunner"
	"goto-bangumi/internal/taskrunner/handlers"
)

// 先实现一下整体的初使化

type Program struct {
	ctx        context.Context
	cancel     context.CancelFunc
	db         *database.DB
	downloader *download.DownloadClient
}

func InitProgram(ctx context.Context) *Program {
	// Load config
	if err := conf.Init(); err != nil {
		slog.Error("[program] 加载配置文件失败", "error", err)
		panic(err)
	}

	cfg := conf.Get()

	// Initialize logger
	logger.Init()
	if cfg.Program.DebugEnable {
		logger.SetLevel(slog.LevelDebug)
	}

	// Initialize database
	db, err := database.NewDB(nil)
	if err != nil {
		slog.Error("[program] 初始化数据库失败", "error", err)
		panic(err)
	}

	// Initialize modules with injected config
	network.Init(&cfg.Proxy)
	parser.Init(&cfg.Parser)
	notification.NotificationClient.Init(&cfg.Notification)
	rename.Init(&cfg.Rename)

	downloader := download.NewDownloadClient()
	downloader.Init(&cfg.Downloader)

	return &Program{db: db, downloader: downloader}
}

func (p *Program) Start(ctx context.Context) {
	p.ctx, p.cancel = context.WithCancel(ctx)

	// 启动时检查一次下载器, 失败只记录日志
	go func() {
		if err := p.downloader.CheckConnection(p.ctx); err != nil {
			slog.Error("[program] 下载器检查失败", "error", err)
		}
	}()

	// 创建并启动 taskrunner
	renamer := rename.New(p.db, p.downloader)
	refresher := refresh.New(p.db)
	runner := taskrunner.New(8, 4)
	runner.Register(model.PhaseAdding, handlers.NewAddHandler(p.downloader))                    // 唯一受限阶段（持有流水线槽位）
	runner.Register(model.PhaseChecking, handlers.NewCheckHandler(p.db, p.downloader))          // 轻量查询
	runner.Register(model.PhaseDownloading, handlers.NewDownloadingHandler(p.db, p.downloader)) // 轻量轮询
	runner.Register(model.PhaseRenaming, handlers.NewRenameHandler(p.db, renamer))              // 本地文件操作
	runner.Start(p.ctx)

	// 启动调度器
	InitScheduler(p.ctx, runner, p.db, refresher)
}

func (p *Program) Stop() {
	p.cancel()
	if p.db != nil {
		if err := p.db.Close(); err != nil {
			slog.Error("[program] 关闭数据库失败", "error", err)
		}
	}
	slog.Info("程序已停止")
}

// InitScheduler 初始化并启动调度器
func InitScheduler(ctx context.Context, runner *taskrunner.TaskRunner, db *database.DB, refresher *refresh.Refresher) {
	scheduler.InitScheduler(ctx)

	s := scheduler.GetScheduler()
	if s == nil {
		slog.Error("调度器初始化失败")
		return
	}

	s.AddTask(task.NewRSSRefreshTask(conf.Get().Program, runner, db, refresher))

	s.Start()

	slog.Info("调度器启动成功")
}

//TODO: 日志更新的时候要知道是哪一部分更新了,然后要对哪一部分进行重新初始化
