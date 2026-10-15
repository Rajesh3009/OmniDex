package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type TorrentRecord struct {
	InfoHash    string    `json:"info_hash"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	FileCount   int       `json:"file_count"`
	FilesJSON   string    `json:"files_json,omitempty"`
	PieceLength int64     `json:"piece_length"`
	CreatedTime time.Time `json:"created_time"`
	Source      string    `json:"source"` // "dht" or "prowlarr"
	Seeders     int       `json:"seeders"`
	Leechers    int       `json:"leechers"`
	Category    string    `json:"category"`
	MagnetURI   string    `json:"magnet_uri"`
}

type Stats struct {
	TotalTorrents int64 `json:"total_torrents"`
	DHTCount      int64 `json:"dht_count"`
	ProwlarrCount int64 `json:"prowlarr_count"`
	TotalSizeBytes int64 `json:"total_size_bytes"`
}

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	// Enable WAL mode and foreign keys for high concurrent reading & writing
	connStr := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)", dbPath)
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite single writer safety
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS torrents (
		info_hash TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		size INTEGER NOT NULL DEFAULT 0,
		file_count INTEGER NOT NULL DEFAULT 1,
		files_json TEXT,
		piece_length INTEGER DEFAULT 0,
		created_time DATETIME NOT NULL,
		source TEXT NOT NULL DEFAULT 'dht',
		seeders INTEGER DEFAULT 0,
		leechers INTEGER DEFAULT 0,
		category TEXT DEFAULT 'Other',
		magnet_uri TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_torrents_created ON torrents(created_time DESC);
	CREATE INDEX IF NOT EXISTS idx_torrents_size ON torrents(size DESC);
	CREATE INDEX IF NOT EXISTS idx_torrents_source ON torrents(source);

	CREATE VIRTUAL TABLE IF NOT EXISTS torrents_fts USING fts5(
		name,
		info_hash UNINDEXED,
		content='torrents',
		content_rowid='rowid',
		tokenize = 'unicode61 remove_diacritics 2'
	);

	-- Triggers to keep FTS index synced with torrents table
	CREATE TRIGGER IF NOT EXISTS torrents_ai AFTER INSERT ON torrents BEGIN
		INSERT INTO torrents_fts(rowid, name, info_hash) VALUES (new.rowid, new.name, new.info_hash);
	END;

	CREATE TRIGGER IF NOT EXISTS torrents_ad AFTER DELETE ON torrents BEGIN
		INSERT INTO torrents_fts(torrents_fts, rowid, name, info_hash) VALUES('delete', old.rowid, old.name, old.info_hash);
	END;

	CREATE TRIGGER IF NOT EXISTS torrents_au AFTER UPDATE ON torrents BEGIN
		INSERT INTO torrents_fts(torrents_fts, rowid, name, info_hash) VALUES('delete', old.rowid, old.name, old.info_hash);
		INSERT INTO torrents_fts(rowid, name, info_hash) VALUES (new.rowid, new.name, new.info_hash);
	END;
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) UpsertTorrent(t *TorrentRecord) error {
	if t.InfoHash == "" {
		return fmt.Errorf("empty info_hash")
	}
	t.InfoHash = strings.ToLower(t.InfoHash)
	if t.CreatedTime.IsZero() {
		t.CreatedTime = time.Now().UTC()
	}

	query := `
	INSERT INTO torrents (
		info_hash, name, size, file_count, files_json, piece_length, created_time, source, seeders, leechers, category, magnet_uri
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(info_hash) DO UPDATE SET
		name = CASE WHEN excluded.name != '' THEN excluded.name ELSE torrents.name END,
		size = CASE WHEN excluded.size > 0 THEN excluded.size ELSE torrents.size END,
		file_count = CASE WHEN excluded.file_count > 0 THEN excluded.file_count ELSE torrents.file_count END,
		files_json = CASE WHEN excluded.files_json != '' THEN excluded.files_json ELSE torrents.files_json END,
		seeders = CASE WHEN excluded.seeders > 0 THEN excluded.seeders ELSE torrents.seeders END,
		leechers = CASE WHEN excluded.leechers > 0 THEN excluded.leechers ELSE torrents.leechers END,
		category = CASE WHEN excluded.category != '' THEN excluded.category ELSE torrents.category END,
		magnet_uri = CASE WHEN excluded.magnet_uri != '' THEN excluded.magnet_uri ELSE torrents.magnet_uri END;
	`
	_, err := s.db.Exec(query,
		t.InfoHash,
		t.Name,
		t.Size,
		t.FileCount,
		t.FilesJSON,
		t.PieceLength,
		t.CreatedTime.Format("2006-01-02 15:04:05"),
		t.Source,
		t.Seeders,
		t.Leechers,
		t.Category,
		t.MagnetURI,
	)
	return err
}

func (s *Store) Exists(infoHash string) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(1) FROM torrents WHERE info_hash = ?", strings.ToLower(infoHash)).Scan(&count)
	return count > 0, err
}

func (s *Store) Search(query string, limit int, offset int) ([]TorrentRecord, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	trimmed := strings.TrimSpace(query)
	var records []TorrentRecord
	var total int64

	if trimmed == "" {
		// Return recent
		err := s.db.QueryRow("SELECT COUNT(1) FROM torrents").Scan(&total)
		if err != nil {
			return nil, 0, err
		}

		rows, err := s.db.Query(`
			SELECT info_hash, name, size, file_count, piece_length, created_time, source, seeders, leechers, category, magnet_uri
			FROM torrents
			ORDER BY created_time DESC
			LIMIT ? OFFSET ?
		`, limit, offset)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()

		for rows.Next() {
			var r TorrentRecord
			var createdStr string
			if err := rows.Scan(&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.PieceLength, &createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI); err != nil {
				return nil, 0, err
			}
			r.CreatedTime, _ = time.Parse("2006-01-02 15:04:05", createdStr)
			records = append(records, r)
		}
		return records, total, nil
	}

	// Clean query for FTS5 (split words, append *)
	words := strings.Fields(trimmed)
	var ftsTokens []string
	for _, w := range words {
		// Escape double quotes and special SQLite FTS chars
		cleaned := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`"*()^:`, r) {
				return -1
			}
			return r
		}, w)
		if cleaned != "" {
			ftsTokens = append(ftsTokens, fmt.Sprintf(`"%s"*`, cleaned))
		}
	}
	ftsExpr := strings.Join(ftsTokens, " ")
	if ftsExpr == "" {
		ftsExpr = `"` + trimmed + `"*`
	}

	countQuery := `
		SELECT COUNT(1)
		FROM torrents_fts f
		JOIN torrents t ON t.rowid = f.rowid
		WHERE torrents_fts MATCH ?
	`
	err := s.db.QueryRow(countQuery, ftsExpr).Scan(&total)
	if err != nil {
		// Fallback to LIKE if FTS fails or syntax error
		likePattern := "%" + trimmed + "%"
		_ = s.db.QueryRow("SELECT COUNT(1) FROM torrents WHERE name LIKE ? OR info_hash LIKE ?", likePattern, likePattern).Scan(&total)
		rows, err2 := s.db.Query(`
			SELECT info_hash, name, size, file_count, piece_length, created_time, source, seeders, leechers, category, magnet_uri
			FROM torrents
			WHERE name LIKE ? OR info_hash LIKE ?
			ORDER BY created_time DESC
			LIMIT ? OFFSET ?
		`, likePattern, likePattern, limit, offset)
		if err2 != nil {
			return nil, 0, err2
		}
		defer rows.Close()
		for rows.Next() {
			var r TorrentRecord
			var createdStr string
			if err := rows.Scan(&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.PieceLength, &createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI); err != nil {
				return nil, 0, err
			}
			r.CreatedTime, _ = time.Parse("2006-01-02 15:04:05", createdStr)
			records = append(records, r)
		}
		return records, total, nil
	}

	searchQuery := `
		SELECT t.info_hash, t.name, t.size, t.file_count, t.piece_length, t.created_time, t.source, t.seeders, t.leechers, t.category, t.magnet_uri
		FROM torrents_fts f
		JOIN torrents t ON t.rowid = f.rowid
		WHERE torrents_fts MATCH ?
		ORDER BY bm25(torrents_fts), t.created_time DESC
		LIMIT ? OFFSET ?
	`
	rows, err := s.db.Query(searchQuery, ftsExpr, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var r TorrentRecord
		var createdStr string
		if err := rows.Scan(&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.PieceLength, &createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI); err != nil {
			return nil, 0, err
		}
		r.CreatedTime, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		records = append(records, r)
	}

	return records, total, nil
}

func (s *Store) GetStats() (Stats, error) {
	var st Stats
	row := s.db.QueryRow(`
		SELECT 
			COUNT(1),
			COALESCE(SUM(CASE WHEN source = 'dht' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN source = 'prowlarr' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(size), 0)
		FROM torrents
	`)
	err := row.Scan(&st.TotalTorrents, &st.DHTCount, &st.ProwlarrCount, &st.TotalSizeBytes)
	return st, err
}

func (s *Store) GetByInfoHash(infoHash string) (*TorrentRecord, error) {
	var r TorrentRecord
	var createdStr string
	err := s.db.QueryRow(`
		SELECT info_hash, name, size, file_count, files_json, piece_length, created_time, source, seeders, leechers, category, magnet_uri
		FROM torrents
		WHERE info_hash = ?
	`, strings.ToLower(infoHash)).Scan(
		&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.FilesJSON, &r.PieceLength,
		&createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI,
	)
	if err != nil {
		return nil, err
	}
	r.CreatedTime, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	return &r, nil
}
