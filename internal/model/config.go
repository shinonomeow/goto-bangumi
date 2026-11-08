package model

// Config is the top-level configuration struct containing all sub-configs.
type Config struct {
	Program      ProgramConfig       `toml:"program" env-prefix:"PROGRAM_"`
	Downloader   DownloaderConfig    `toml:"downloader" env-prefix:"DOWNLOADER_"`
	Parser       RssParserConfig     `toml:"parser" env-prefix:"PARSER_"`
	Rename       BangumiRenameConfig `toml:"rename" env-prefix:"RENAME_"`
	Notification NotificationConfig  `toml:"notification" env-prefix:"NOTIFICATION_"`
	Proxy        ProxyConfig         `toml:"proxy" env-prefix:"PROXY_"`
}

type ProgramConfig struct {
	RssTime     int    `toml:"rss_time" env:"RSS_TIME" env-default:"600"`
	WebuiPort   int    `toml:"webui_port" env:"WEBUI_PORT" env-default:"7892"`
	PassWord    string `toml:"password" env:"PASSWORD" env-default:"adminadmin"`
	DebugEnable bool   `toml:"debug_enable" env:"DEBUG_ENABLE" env-default:"false"`
}

type DownloaderConfig struct {
	Type     string `toml:"type" env:"TYPE" env-default:"qbittorrent"`
	SavePath string `toml:"path" env:"PATH" env-default:"/downloads/Bangumi"`
	MediaPath string `toml:"media_path" env:"MEDIA_PATH" env-default:"/downloads/Bangumi"`
	Host     string `toml:"host" env:"HOST" env-default:"127.0.0.1:8080"`
	Ssl      bool   `toml:"ssl" env:"SSL" env-default:"false"`
	Token    string `toml:"token" env:"TOKEN"`
}

type RssParserConfig struct {
	Enable         bool     `toml:"enable" env:"ENABLE" env-default:"true"`
	Filter         []string `toml:"filter"`
	Include        []string `toml:"include"`
	Language       string   `toml:"language" env:"LANGUAGE" env-default:"zh"`
	MikanCustomURL string   `toml:"mikan_custom_url" env:"MIKAN_CUSTOM_URL" env-default:"mikanani.me"`
	TmdbAPIKey     string   `toml:"tmdb_api_key" env:"TMDB_API_KEY"`
}

type BangumiRenameConfig struct {
	Enable       bool   `toml:"enable" env:"ENABLE" env-default:"true"`
	EpsComplete  bool   `toml:"eps_complete" env:"EPS_COMPLETE" env-default:"false"`
	RenameMethod string `toml:"rename_method" env:"RENAME_METHOD" env-default:"advanced"`
	Year         bool   `toml:"year" env:"YEAR" env-default:"false"`
	Group        bool   `toml:"group" env:"GROUP" env-default:"false"`
}

type NotificationConfig struct {
	Enable bool   `toml:"enable" env:"ENABLE" env-default:"false"`
	Type   string `toml:"type" env:"TYPE" env-default:"telegram"`
	Token  string `toml:"token" env:"TOKEN"`
	ChatID string `toml:"chat_id" env:"CHAT_ID"`
}
