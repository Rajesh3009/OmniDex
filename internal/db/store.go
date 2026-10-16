package db

import (
	"database/sql"
	"fmt"
	"os"
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
	TotalTorrents  int64 `json:"total_torrents"`
	DHTCount       int64 `json:"dht_count"`
	ProwlarrCount  int64 `json:"prowlarr_count"`
	TotalSizeBytes int64 `json:"total_size_bytes"`
	DBSizeBytes    int64 `json:"db_size_bytes"`
}

type SearchFilter struct {
	Query    string `json:"query"`
	Category string `json:"category"`
	Source   string `json:"source"`
	MinSize  int64  `json:"min_size"`
	MaxSize  int64  `json:"max_size"`
	SortBy   string `json:"sort_by"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

type Store struct {
	db     *sql.DB
	dbPath string
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

	s := &Store{db: db, dbPath: dbPath}
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

func parseDBTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func buildOrderBy(sortBy string, hasFTS bool) string {
	prefix := ""
	if hasFTS {
		prefix = "t."
	}
	switch strings.ToLower(sortBy) {
	case "created_asc":
		return prefix + "created_time ASC"
	case "created_desc":
		return prefix + "created_time DESC"
	case "size_asc":
		return prefix + "size ASC, " + prefix + "created_time DESC"
	case "size_desc":
		return prefix + "size DESC, " + prefix + "created_time DESC"
	case "name_asc":
		return prefix + "name COLLATE NOCASE ASC"
	case "name_desc":
		return prefix + "name COLLATE NOCASE DESC"
	case "seeders_desc":
		return prefix + "seeders DESC, " + prefix + "created_time DESC"
	case "seeders_asc":
		return prefix + "seeders ASC, " + prefix + "created_time DESC"
	case "relevance":
		if hasFTS {
			return "bm25(torrents_fts), t.created_time DESC"
		}
		return prefix + "created_time DESC"
	default:
		if hasFTS {
			return "bm25(torrents_fts), t.created_time DESC"
		}
		return prefix + "created_time DESC"
	}
}

func (s *Store) SearchWithFilter(filter SearchFilter) ([]TorrentRecord, int64, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	trimmed := strings.TrimSpace(filter.Query)
	var records []TorrentRecord
	var total int64

	// Build common filter conditions
	var whereClauses []string
	var args []any

	if filter.Category != "" && !strings.EqualFold(filter.Category, "all") {
		whereClauses = append(whereClauses, "category = ?")
		args = append(args, filter.Category)
	}
	if filter.Source != "" && !strings.EqualFold(filter.Source, "all") {
		whereClauses = append(whereClauses, "source = ?")
		args = append(args, strings.ToLower(filter.Source))
	}
	if filter.MinSize > 0 {
		whereClauses = append(whereClauses, "size >= ?")
		args = append(args, filter.MinSize)
	}
	if filter.MaxSize > 0 {
		whereClauses = append(whereClauses, "size <= ?")
		args = append(args, filter.MaxSize)
	}

	if trimmed == "" {
		whereSQL := ""
		if len(whereClauses) > 0 {
			whereSQL = " WHERE " + strings.Join(whereClauses, " AND ")
		}

		countQuery := "SELECT COUNT(1) FROM torrents" + whereSQL
		err := s.db.QueryRow(countQuery, args...).Scan(&total)
		if err != nil {
			return nil, 0, err
		}

		orderBy := buildOrderBy(filter.SortBy, false)
		querySQL := fmt.Sprintf(`
			SELECT info_hash, name, size, file_count, piece_length, created_time, source, seeders, leechers, category, magnet_uri
			FROM torrents
			%s
			ORDER BY %s
			LIMIT ? OFFSET ?
		`, whereSQL, orderBy)

		queryArgs := append(args, filter.Limit, filter.Offset)
		rows, err := s.db.Query(querySQL, queryArgs...)
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
			r.CreatedTime = parseDBTime(createdStr)
			records = append(records, r)
		}
		return records, total, nil
	}

	// Clean query for FTS5 (split words, append *)
	words := strings.Fields(trimmed)
	var ftsTokens []string
	for _, w := range words {
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

	ftsWhereClauses := []string{"torrents_fts MATCH ?"}
	ftsArgs := []any{ftsExpr}

	if filter.Category != "" && !strings.EqualFold(filter.Category, "all") {
		ftsWhereClauses = append(ftsWhereClauses, "t.category = ?")
		ftsArgs = append(ftsArgs, filter.Category)
	}
	if filter.Source != "" && !strings.EqualFold(filter.Source, "all") {
		ftsWhereClauses = append(ftsWhereClauses, "t.source = ?")
		ftsArgs = append(ftsArgs, strings.ToLower(filter.Source))
	}
	if filter.MinSize > 0 {
		ftsWhereClauses = append(ftsWhereClauses, "t.size >= ?")
		ftsArgs = append(ftsArgs, filter.MinSize)
	}
	if filter.MaxSize > 0 {
		ftsWhereClauses = append(ftsWhereClauses, "t.size <= ?")
		ftsArgs = append(ftsArgs, filter.MaxSize)
	}

	ftsWhereSQL := " WHERE " + strings.Join(ftsWhereClauses, " AND ")
	countQuery := `
		SELECT COUNT(1)
		FROM torrents_fts f
		JOIN torrents t ON t.rowid = f.rowid
	` + ftsWhereSQL

	err := s.db.QueryRow(countQuery, ftsArgs...).Scan(&total)
	if err != nil {
		// Fallback to LIKE if FTS fails or syntax error
		likePattern := "%" + trimmed + "%"
		fallbackWhere := []string{"(name LIKE ? OR info_hash LIKE ?)"}
		fallbackArgs := []any{likePattern, likePattern}

		if filter.Category != "" && !strings.EqualFold(filter.Category, "all") {
			fallbackWhere = append(fallbackWhere, "category = ?")
			fallbackArgs = append(fallbackArgs, filter.Category)
		}
		if filter.Source != "" && !strings.EqualFold(filter.Source, "all") {
			fallbackWhere = append(fallbackWhere, "source = ?")
			fallbackArgs = append(fallbackArgs, strings.ToLower(filter.Source))
		}
		if filter.MinSize > 0 {
			fallbackWhere = append(fallbackWhere, "size >= ?")
			fallbackArgs = append(fallbackArgs, filter.MinSize)
		}
		if filter.MaxSize > 0 {
			fallbackWhere = append(fallbackWhere, "size <= ?")
			fallbackArgs = append(fallbackArgs, filter.MaxSize)
		}

		fbWhereSQL := " WHERE " + strings.Join(fallbackWhere, " AND ")
		_ = s.db.QueryRow("SELECT COUNT(1) FROM torrents"+fbWhereSQL, fallbackArgs...).Scan(&total)

		orderBy := buildOrderBy(filter.SortBy, false)
		querySQL := fmt.Sprintf(`
			SELECT info_hash, name, size, file_count, piece_length, created_time, source, seeders, leechers, category, magnet_uri
			FROM torrents
			%s
			ORDER BY %s
			LIMIT ? OFFSET ?
		`, fbWhereSQL, orderBy)

		fbArgs := append(fallbackArgs, filter.Limit, filter.Offset)
		rows, err2 := s.db.Query(querySQL, fbArgs...)
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
			r.CreatedTime = parseDBTime(createdStr)
			records = append(records, r)
		}
		return records, total, nil
	}

	orderBy := buildOrderBy(filter.SortBy, true)
	searchQuery := fmt.Sprintf(`
		SELECT t.info_hash, t.name, t.size, t.file_count, t.piece_length, t.created_time, t.source, t.seeders, t.leechers, t.category, t.magnet_uri
		FROM torrents_fts f
		JOIN torrents t ON t.rowid = f.rowid
		%s
		ORDER BY %s
		LIMIT ? OFFSET ?
	`, ftsWhereSQL, orderBy)

	finalArgs := append(ftsArgs, filter.Limit, filter.Offset)
	rows, err := s.db.Query(searchQuery, finalArgs...)
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
		r.CreatedTime = parseDBTime(createdStr)
		records = append(records, r)
	}

	return records, total, nil
}

func (s *Store) Search(query string, limit int, offset int) ([]TorrentRecord, int64, error) {
	return s.SearchWithFilter(SearchFilter{
		Query:  query,
		Limit:  limit,
		Offset: offset,
	})
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
	if err == nil {
		st.DBSizeBytes = s.getDiskSize()
	}
	return st, err
}

func (s *Store) getDiskSize() int64 {
	var total int64
	for _, p := range []string{s.dbPath, s.dbPath + "-wal", s.dbPath + "-shm"} {
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	return total
}

func (s *Store) GetCategories() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT category FROM torrents WHERE category != '' ORDER BY category ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cats []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err == nil && c != "" {
			cats = append(cats, c)
		}
	}
	return cats, nil
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
	r.CreatedTime = parseDBTime(createdStr)
	return &r, nil
}
