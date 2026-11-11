package config

type DownloadClientConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`      // "qbittorrent", "transmission", "deluge"
	Host      string `json:"host"`      // e.g. "http://localhost:8080"
	Username  string `json:"username"`
	Password  string `json:"password"`
	SavePath  string `json:"save_path"`  // optional default save path
	Category  string `json:"category"`   // optional default category
	AutoStart bool   `json:"auto_start"` // true = start download immediately, false = paused
	IsDefault bool   `json:"is_default"`
	Enabled   bool   `json:"enabled"`
}
