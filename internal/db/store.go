package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"local-indexer/internal/metadata"
	"local-indexer/internal/spamfilter"
)

type TorrentRecord struct {
	InfoHash     string    `json:"info_hash"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	FileCount    int       `json:"file_count"`
	FilesJSON    string    `json:"files_json,omitempty"`
	PieceLength  int64     `json:"piece_length"`
	CreatedTime  time.Time `json:"created_time"`
	LastSeen     time.Time `json:"last_seen"`
	Source       string    `json:"source"` // "dht" or "prowlarr" or "feed"
	Seeders      int       `json:"seeders"`
	Leechers     int       `json:"leechers"`
	Category     string    `json:"category"`
	MagnetURI    string    `json:"magnet_uri"`
	Resolution   string    `json:"resolution,omitempty"`
	Codec        string    `json:"codec,omitempty"`
	Quality      string    `json:"quality,omitempty"`
	Audio        string    `json:"audio,omitempty"`
	HDR          string    `json:"hdr,omitempty"`
	ReleaseGroup string    `json:"release_group,omitempty"`
	IsSuspicious bool      `json:"is_suspicious"`
	SpamScore    int       `json:"spam_score"`
	SpamReasons  string    `json:"spam_reasons,omitempty"`
}

type Stats struct {
	TotalTorrents     int64 `json:"total_torrents"`
	DHTCount          int64 `json:"dht_count"`
	ProwlarrCount     int64 `json:"prowlarr_count"`
	FeedCount         int64 `json:"feed_count"`
	SuspiciousCount   int64 `json:"suspicious_count"`
	TotalSizeBytes    int64 `json:"total_size_bytes"`
	DBSizeBytes       int64 `json:"db_size_bytes"`
}

type BandwidthStats struct {
	TotalDownloadedBytes   int64  `json:"total_downloaded_bytes"`
	TotalUploadedBytes     int64  `json:"total_uploaded_bytes"`
	MonthlyDownloadedBytes int64  `json:"monthly_downloaded_bytes"`
	MonthlyUploadedBytes   int64  `json:"monthly_uploaded_bytes"`
	CurrentMonth           string `json:"current_month"`
}

type SearchFilter struct {
	Query             string `json:"query"`
	Category          string `json:"category"`
	Source            string `json:"source"`
	Resolution        string `json:"resolution"`
	Codec             string `json:"codec"`
	Quality           string `json:"quality"`
	ReleaseGroup      string `json:"release_group"`
	IncludeSuspicious bool   `json:"include_suspicious"`
	MinSize           int64  `json:"min_size"`
	MaxSize           int64  `json:"max_size"`
	SortBy            string `json:"sort_by"`
	Limit             int    `json:"limit"`
	Offset            int    `json:"offset"`
}

type Store struct {
	db     *sql.DB
	dbPath string
}

func NewStore(dbPath string) (*Store, error) {
	// Optimized pragmas: WAL mode, 8MB cache per connection, 32MB max mmap, busy timeout
	connStr := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=cache_size(-8000)&_pragma=mmap_size(33554432)&_pragma=temp_store(MEMORY)", dbPath)
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(4) // Controlled connection pool to prevent cache proliferation
	db.SetMaxIdleConns(2)

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
		last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		source TEXT NOT NULL DEFAULT 'dht',
		seeders INTEGER DEFAULT 0,
		leechers INTEGER DEFAULT 0,
		category TEXT DEFAULT 'Other',
		magnet_uri TEXT,
		resolution TEXT DEFAULT '',
		codec TEXT DEFAULT '',
		quality TEXT DEFAULT '',
		audio TEXT DEFAULT '',
		hdr TEXT DEFAULT '',
		release_group TEXT DEFAULT '',
		is_suspicious INTEGER NOT NULL DEFAULT 0,
		spam_score INTEGER NOT NULL DEFAULT 0,
		spam_reasons TEXT DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_torrents_created ON torrents(created_time DESC);
	CREATE INDEX IF NOT EXISTS idx_torrents_size ON torrents(size DESC);
	CREATE INDEX IF NOT EXISTS idx_torrents_source ON torrents(source);
	CREATE INDEX IF NOT EXISTS idx_torrents_category ON torrents(category);
	CREATE INDEX IF NOT EXISTS idx_torrents_seeders ON torrents(seeders DESC);

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
	if err != nil {
		return err
	}

	// Schema migrations for existing databases
	columns := []string{
		"ALTER TABLE torrents ADD COLUMN last_seen DATETIME DEFAULT CURRENT_TIMESTAMP",
		"ALTER TABLE torrents ADD COLUMN resolution TEXT DEFAULT ''",
		"ALTER TABLE torrents ADD COLUMN codec TEXT DEFAULT ''",
		"ALTER TABLE torrents ADD COLUMN quality TEXT DEFAULT ''",
		"ALTER TABLE torrents ADD COLUMN audio TEXT DEFAULT ''",
		"ALTER TABLE torrents ADD COLUMN hdr TEXT DEFAULT ''",
		"ALTER TABLE torrents ADD COLUMN release_group TEXT DEFAULT ''",
		"ALTER TABLE torrents ADD COLUMN is_suspicious INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE torrents ADD COLUMN spam_score INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE torrents ADD COLUMN spam_reasons TEXT DEFAULT ''",
	}
	for _, colSQL := range columns {
		_, _ = s.db.Exec(colSQL)
	}

	// Backfill last_seen for legacy records where null or empty
	_, _ = s.db.Exec("UPDATE torrents SET last_seen = created_time WHERE last_seen IS NULL OR last_seen = ''")

	// Create indices for new columns after ensuring columns exist
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_torrents_last_seen ON torrents(last_seen DESC)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_torrents_res ON torrents(resolution)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_torrents_codec ON torrents(codec)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_torrents_qual ON torrents(quality)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_torrents_grp ON torrents(release_group)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_torrents_suspicious ON torrents(is_suspicious)")

	// Ensure DHT records have at least 1 seeder since an announcer reached our node
	_, _ = s.db.Exec("UPDATE torrents SET seeders = 1 WHERE source = 'dht' AND seeders <= 0")

	// Bandwidth stats persistent storage table with monthly tracking
	_, _ = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS bandwidth_stats (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			downloaded_bytes INTEGER NOT NULL DEFAULT 0,
			uploaded_bytes INTEGER NOT NULL DEFAULT 0,
			monthly_downloaded_bytes INTEGER NOT NULL DEFAULT 0,
			monthly_uploaded_bytes INTEGER NOT NULL DEFAULT 0,
			current_month TEXT NOT NULL DEFAULT '',
			updated_at DATETIME NOT NULL
		);
		INSERT OR IGNORE INTO bandwidth_stats (id, downloaded_bytes, uploaded_bytes, monthly_downloaded_bytes, monthly_uploaded_bytes, current_month, updated_at)
		VALUES (1, 0, 0, 0, 0, strftime('%Y-%m', 'now'), CURRENT_TIMESTAMP);
	`)
	_, _ = s.db.Exec("ALTER TABLE bandwidth_stats ADD COLUMN monthly_downloaded_bytes INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE bandwidth_stats ADD COLUMN monthly_uploaded_bytes INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE bandwidth_stats ADD COLUMN current_month TEXT NOT NULL DEFAULT ''")

	// Asynchronously backfill tags and spam checks for existing records
	go s.backfillMetadataTags()

	return nil
}

func (s *Store) backfillMetadataTags() {
	rows, err := s.db.Query(`
		SELECT info_hash, name, size, category, COALESCE(files_json, '')
		FROM torrents
		WHERE resolution = '' AND codec = '' AND quality = '' AND spam_score = 0
		LIMIT 2000
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	type item struct {
		hash      string
		name      string
		size      int64
		category  string
		filesJSON string
	}
	var batch []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.hash, &it.name, &it.size, &it.category, &it.filesJSON); err == nil {
			batch = append(batch, it)
		}
	}
	_ = rows.Close()

	if len(batch) == 0 {
		return
	}

	stmt, err := s.db.Prepare(`
		UPDATE torrents
		SET resolution = ?, codec = ?, quality = ?, audio = ?, hdr = ?, release_group = ?,
		    is_suspicious = ?, spam_score = ?, spam_reasons = ?
		WHERE info_hash = ?
	`)
	if err != nil {
		return
	}
	defer stmt.Close()

	for _, it := range batch {
		var files []string
		if it.filesJSON != "" {
			_ = json.Unmarshal([]byte(it.filesJSON), &files)
		}
		tags := metadata.ExtractTags(it.name, files)
		spamVerdict := spamfilter.Inspect(it.name, files, it.category, it.size)
		isSuspInt := 0
		if spamVerdict.IsSuspicious {
			isSuspInt = 1
		}
		reasonsStr := strings.Join(spamVerdict.Reasons, " • ")
		_, _ = stmt.Exec(tags.Resolution, tags.Codec, tags.Quality, tags.Audio, tags.HDR, tags.ReleaseGroup,
			isSuspInt, spamVerdict.Score, reasonsStr, it.hash)
	}
}

func (s *Store) UpsertTorrent(t *TorrentRecord) error {
	if t.InfoHash == "" {
		return fmt.Errorf("empty info_hash")
	}
	t.InfoHash = strings.ToLower(t.InfoHash)
	if t.CreatedTime.IsZero() {
		t.CreatedTime = time.Now().UTC()
	}
	if t.LastSeen.IsZero() {
		t.LastSeen = time.Now().UTC()
	}

	isSuspInt := 0
	if t.IsSuspicious {
		isSuspInt = 1
	}

	query := `
	INSERT INTO torrents (
		info_hash, name, size, file_count, files_json, piece_length, created_time, last_seen, source, seeders, leechers, category, magnet_uri,
		resolution, codec, quality, audio, hdr, release_group, is_suspicious, spam_score, spam_reasons
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(info_hash) DO UPDATE SET
		name = CASE WHEN excluded.name != '' THEN excluded.name ELSE torrents.name END,
		size = CASE WHEN excluded.size > 0 THEN excluded.size ELSE torrents.size END,
		file_count = CASE WHEN excluded.file_count > 0 THEN excluded.file_count ELSE torrents.file_count END,
		files_json = CASE WHEN excluded.files_json != '' THEN excluded.files_json ELSE torrents.files_json END,
		last_seen = CURRENT_TIMESTAMP,
		seeders = CASE WHEN excluded.seeders > 0 THEN excluded.seeders ELSE torrents.seeders END,
		leechers = CASE WHEN excluded.leechers > 0 THEN excluded.leechers ELSE torrents.leechers END,
		category = CASE WHEN excluded.category != '' THEN excluded.category ELSE torrents.category END,
		magnet_uri = CASE WHEN excluded.magnet_uri != '' THEN excluded.magnet_uri ELSE torrents.magnet_uri END,
		resolution = CASE WHEN excluded.resolution != '' THEN excluded.resolution ELSE torrents.resolution END,
		codec = CASE WHEN excluded.codec != '' THEN excluded.codec ELSE torrents.codec END,
		quality = CASE WHEN excluded.quality != '' THEN excluded.quality ELSE torrents.quality END,
		audio = CASE WHEN excluded.audio != '' THEN excluded.audio ELSE torrents.audio END,
		hdr = CASE WHEN excluded.hdr != '' THEN excluded.hdr ELSE torrents.hdr END,
		release_group = CASE WHEN excluded.release_group != '' THEN excluded.release_group ELSE torrents.release_group END,
		is_suspicious = CASE WHEN excluded.is_suspicious != 0 THEN excluded.is_suspicious ELSE torrents.is_suspicious END,
		spam_score = CASE WHEN excluded.spam_score > 0 THEN excluded.spam_score ELSE torrents.spam_score END,
		spam_reasons = CASE WHEN excluded.spam_reasons != '' THEN excluded.spam_reasons ELSE torrents.spam_reasons END;
	`
	_, err := s.db.Exec(query,
		t.InfoHash,
		t.Name,
		t.Size,
		t.FileCount,
		t.FilesJSON,
		t.PieceLength,
		t.CreatedTime.Format("2006-01-02 15:04:05"),
		t.LastSeen.Format("2006-01-02 15:04:05"),
		t.Source,
		t.Seeders,
		t.Leechers,
		t.Category,
		t.MagnetURI,
		t.Resolution,
		t.Codec,
		t.Quality,
		t.Audio,
		t.HDR,
		t.ReleaseGroup,
		isSuspInt,
		t.SpamScore,
		t.SpamReasons,
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
	if filter.Resolution != "" && !strings.EqualFold(filter.Resolution, "all") {
		whereClauses = append(whereClauses, "resolution = ?")
		args = append(args, filter.Resolution)
	}
	if filter.Codec != "" && !strings.EqualFold(filter.Codec, "all") {
		whereClauses = append(whereClauses, "codec = ?")
		args = append(args, filter.Codec)
	}
	if filter.Quality != "" && !strings.EqualFold(filter.Quality, "all") {
		whereClauses = append(whereClauses, "quality = ?")
		args = append(args, filter.Quality)
	}
	if filter.ReleaseGroup != "" && !strings.EqualFold(filter.ReleaseGroup, "all") {
		whereClauses = append(whereClauses, "release_group = ?")
		args = append(args, filter.ReleaseGroup)
	}
	if !filter.IncludeSuspicious {
		whereClauses = append(whereClauses, "is_suspicious = 0")
	}
	if filter.MinSize > 0 {
		whereClauses = append(whereClauses, "size >= ?")
		args = append(args, filter.MinSize)
	}
	if filter.MaxSize > 0 {
		whereClauses = append(whereClauses, "size <= ?")
		args = append(args, filter.MaxSize)
	}

	selectCols := "info_hash, name, size, file_count, piece_length, created_time, source, seeders, leechers, category, magnet_uri, COALESCE(files_json, ''), resolution, codec, quality, audio, hdr, release_group, is_suspicious, spam_score, COALESCE(spam_reasons, '')"
	selectColsT := "t.info_hash, t.name, t.size, t.file_count, t.piece_length, t.created_time, t.source, t.seeders, t.leechers, t.category, t.magnet_uri, COALESCE(t.files_json, ''), t.resolution, t.codec, t.quality, t.audio, t.hdr, t.release_group, t.is_suspicious, t.spam_score, COALESCE(t.spam_reasons, '')"

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
			SELECT %s
			FROM torrents
			%s
			ORDER BY %s
			LIMIT ? OFFSET ?
		`, selectCols, whereSQL, orderBy)

		queryArgs := append(args, filter.Limit, filter.Offset)
		rows, err := s.db.Query(querySQL, queryArgs...)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()

		for rows.Next() {
			var r TorrentRecord
			var createdStr string
			var isSuspInt int
			if err := rows.Scan(&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.PieceLength, &createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI, &r.FilesJSON, &r.Resolution, &r.Codec, &r.Quality, &r.Audio, &r.HDR, &r.ReleaseGroup, &isSuspInt, &r.SpamScore, &r.SpamReasons); err != nil {
				return nil, 0, err
			}
			r.IsSuspicious = isSuspInt == 1
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
	if filter.Resolution != "" && !strings.EqualFold(filter.Resolution, "all") {
		ftsWhereClauses = append(ftsWhereClauses, "t.resolution = ?")
		ftsArgs = append(ftsArgs, filter.Resolution)
	}
	if filter.Codec != "" && !strings.EqualFold(filter.Codec, "all") {
		ftsWhereClauses = append(ftsWhereClauses, "t.codec = ?")
		ftsArgs = append(ftsArgs, filter.Codec)
	}
	if filter.Quality != "" && !strings.EqualFold(filter.Quality, "all") {
		ftsWhereClauses = append(ftsWhereClauses, "t.quality = ?")
		ftsArgs = append(ftsArgs, filter.Quality)
	}
	if filter.ReleaseGroup != "" && !strings.EqualFold(filter.ReleaseGroup, "all") {
		ftsWhereClauses = append(ftsWhereClauses, "t.release_group = ?")
		ftsArgs = append(ftsArgs, filter.ReleaseGroup)
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
		if filter.Resolution != "" && !strings.EqualFold(filter.Resolution, "all") {
			fallbackWhere = append(fallbackWhere, "resolution = ?")
			fallbackArgs = append(fallbackArgs, filter.Resolution)
		}
		if filter.Codec != "" && !strings.EqualFold(filter.Codec, "all") {
			fallbackWhere = append(fallbackWhere, "codec = ?")
			fallbackArgs = append(fallbackArgs, filter.Codec)
		}
		if filter.Quality != "" && !strings.EqualFold(filter.Quality, "all") {
			fallbackWhere = append(fallbackWhere, "quality = ?")
			fallbackArgs = append(fallbackArgs, filter.Quality)
		}
		if filter.ReleaseGroup != "" && !strings.EqualFold(filter.ReleaseGroup, "all") {
			fallbackWhere = append(fallbackWhere, "release_group = ?")
			fallbackArgs = append(fallbackArgs, filter.ReleaseGroup)
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
			SELECT %s
			FROM torrents
			%s
			ORDER BY %s
			LIMIT ? OFFSET ?
		`, selectCols, fbWhereSQL, orderBy)

		fbArgs := append(fallbackArgs, filter.Limit, filter.Offset)
		rows, err2 := s.db.Query(querySQL, fbArgs...)
		if err2 != nil {
			return nil, 0, err2
		}
		defer rows.Close()

		for rows.Next() {
			var r TorrentRecord
			var createdStr string
			var isSuspInt int
			if err := rows.Scan(&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.PieceLength, &createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI, &r.FilesJSON, &r.Resolution, &r.Codec, &r.Quality, &r.Audio, &r.HDR, &r.ReleaseGroup, &isSuspInt, &r.SpamScore, &r.SpamReasons); err != nil {
				return nil, 0, err
			}
			r.IsSuspicious = isSuspInt == 1
			r.CreatedTime = parseDBTime(createdStr)
			records = append(records, r)
		}
		return records, total, nil
	}

	orderBy := buildOrderBy(filter.SortBy, true)
	searchQuery := fmt.Sprintf(`
		SELECT %s
		FROM torrents_fts f
		JOIN torrents t ON t.rowid = f.rowid
		%s
		ORDER BY %s
		LIMIT ? OFFSET ?
	`, selectColsT, ftsWhereSQL, orderBy)

	finalArgs := append(ftsArgs, filter.Limit, filter.Offset)
	rows, err := s.db.Query(searchQuery, finalArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var r TorrentRecord
		var createdStr string
		var isSuspInt int
		if err := rows.Scan(&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.PieceLength, &createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI, &r.FilesJSON, &r.Resolution, &r.Codec, &r.Quality, &r.Audio, &r.HDR, &r.ReleaseGroup, &isSuspInt, &r.SpamScore, &r.SpamReasons); err != nil {
			return nil, 0, err
		}
		r.IsSuspicious = isSuspInt == 1
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
			COALESCE(SUM(CASE WHEN source NOT IN ('dht', 'prowlarr') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN is_suspicious = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(size), 0)
		FROM torrents
	`)
	err := row.Scan(&st.TotalTorrents, &st.DHTCount, &st.ProwlarrCount, &st.FeedCount, &st.SuspiciousCount, &st.TotalSizeBytes)
	if err == nil {
		st.DBSizeBytes = s.getDiskSize()
	}
	return st, err
}

func (s *Store) GetBandwidthStats() (BandwidthStats, error) {
	var bs BandwidthStats
	nowMonth := time.Now().UTC().Format("2006-01")
	row := s.db.QueryRow(`
		SELECT downloaded_bytes, uploaded_bytes, COALESCE(monthly_downloaded_bytes, 0), COALESCE(monthly_uploaded_bytes, 0), COALESCE(current_month, '')
		FROM bandwidth_stats
		WHERE id = 1
	`)
	err := row.Scan(&bs.TotalDownloadedBytes, &bs.TotalUploadedBytes, &bs.MonthlyDownloadedBytes, &bs.MonthlyUploadedBytes, &bs.CurrentMonth)
	if err != nil {
		return bs, err
	}

	// If calendar month rolled over, reset monthly counters
	if bs.CurrentMonth != nowMonth {
		_, _ = s.db.Exec(`
			UPDATE bandwidth_stats
			SET monthly_downloaded_bytes = 0,
			    monthly_uploaded_bytes = 0,
			    current_month = ?,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = 1
		`, nowMonth)
		bs.MonthlyDownloadedBytes = 0
		bs.MonthlyUploadedBytes = 0
		bs.CurrentMonth = nowMonth
	}

	return bs, nil
}

func (s *Store) AddBandwidthStats(downloadedBytes, uploadedBytes int64) error {
	if downloadedBytes <= 0 && uploadedBytes <= 0 {
		return nil
	}
	nowMonth := time.Now().UTC().Format("2006-01")

	// Check if month rolled over
	var currentMonth string
	_ = s.db.QueryRow(`SELECT COALESCE(current_month, '') FROM bandwidth_stats WHERE id = 1`).Scan(&currentMonth)

	if currentMonth != nowMonth {
		_, err := s.db.Exec(`
			UPDATE bandwidth_stats
			SET downloaded_bytes = downloaded_bytes + ?,
			    uploaded_bytes = uploaded_bytes + ?,
			    monthly_downloaded_bytes = ?,
			    monthly_uploaded_bytes = ?,
			    current_month = ?,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = 1
		`, downloadedBytes, uploadedBytes, downloadedBytes, uploadedBytes, nowMonth)
		return err
	}

	_, err := s.db.Exec(`
		UPDATE bandwidth_stats 
		SET downloaded_bytes = downloaded_bytes + ?,
		    uploaded_bytes = uploaded_bytes + ?,
		    monthly_downloaded_bytes = monthly_downloaded_bytes + ?,
		    monthly_uploaded_bytes = monthly_uploaded_bytes + ?,
		    current_month = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, downloadedBytes, uploadedBytes, downloadedBytes, uploadedBytes, nowMonth)
	return err
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

func (s *Store) GetTorrentByHash(hash string) (*TorrentRecord, error) {
	cleanHash := strings.ToLower(strings.TrimSpace(hash))
	row := s.db.QueryRow(`
		SELECT info_hash, name, size, file_count, piece_length, created_time, source, seeders, leechers, category, magnet_uri, COALESCE(files_json, ''),
		       resolution, codec, quality, audio, hdr, release_group, is_suspicious, spam_score, COALESCE(spam_reasons, '')
		FROM torrents
		WHERE LOWER(info_hash) = ?
		LIMIT 1
	`, cleanHash)

	var r TorrentRecord
	var createdStr string
	var isSuspInt int
	err := row.Scan(&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.PieceLength, &createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI, &r.FilesJSON,
		&r.Resolution, &r.Codec, &r.Quality, &r.Audio, &r.HDR, &r.ReleaseGroup, &isSuspInt, &r.SpamScore, &r.SpamReasons)
	if err != nil {
		return nil, err
	}
	r.IsSuspicious = isSuspInt == 1
	r.CreatedTime = parseDBTime(createdStr)
	return &r, nil
}

func (s *Store) GetByInfoHash(infoHash string) (*TorrentRecord, error) {
	var r TorrentRecord
	var createdStr string
	var isSuspInt int
	err := s.db.QueryRow(`
		SELECT info_hash, name, size, file_count, files_json, piece_length, created_time, source, seeders, leechers, category, magnet_uri,
		       resolution, codec, quality, audio, hdr, release_group, is_suspicious, spam_score, COALESCE(spam_reasons, '')
		FROM torrents
		WHERE info_hash = ?
	`, strings.ToLower(infoHash)).Scan(
		&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.FilesJSON, &r.PieceLength,
		&createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI,
		&r.Resolution, &r.Codec, &r.Quality, &r.Audio, &r.HDR, &r.ReleaseGroup,
		&isSuspInt, &r.SpamScore, &r.SpamReasons,
	)
	if err != nil {
		return nil, err
	}
	r.IsSuspicious = isSuspInt == 1
	r.CreatedTime = parseDBTime(createdStr)
	return &r, nil
}

// PruneMemory forces SQLite to checkpoint the WAL log and release unused page cache
func (s *Store) PruneMemory() {
	_, _ = s.db.Exec("PRAGMA wal_checkpoint(PASSIVE)")
	_, _ = s.db.Exec("PRAGMA shrink_memory")
}

// DeleteTorrent removes a torrent from SQLite. The FTS5 trigger automatically cleans up torrents_fts.
func (s *Store) DeleteTorrent(infoHash string) error {
	cleanHash := strings.ToLower(strings.TrimSpace(infoHash))
	if cleanHash == "" {
		return fmt.Errorf("empty info_hash")
	}
	res, err := s.db.Exec("DELETE FROM torrents WHERE LOWER(info_hash) = ?", cleanHash)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("torrent not found")
	}
	return nil
}

type PruneOptions struct {
	MaxSeeders int       `json:"max_seeders"` // e.g. 0
	OlderThan  time.Time `json:"older_than"`  // e.g. created/last_seen older than X days
	Source     string    `json:"source"`      // "" for any, or "dht", "prowlarr", "feed"
	PruneSpam  bool      `json:"prune_spam"`  // also prune is_suspicious == 1
}

// PruneTorrents removes dead torrents according to the specified options
func (s *Store) PruneTorrents(opts PruneOptions) (int64, error) {
	var where []string
	var args []any

	if opts.PruneSpam {
		where = append(where, "(seeders <= ? OR is_suspicious = 1)")
		args = append(args, opts.MaxSeeders)
	} else {
		where = append(where, "seeders <= ?")
		args = append(args, opts.MaxSeeders)
	}

	if !opts.OlderThan.IsZero() {
		where = append(where, "created_time <= ?")
		args = append(args, opts.OlderThan.Format("2006-01-02 15:04:05"))
	}

	if opts.Source != "" && !strings.EqualFold(opts.Source, "all") {
		where = append(where, "source = ?")
		args = append(args, strings.ToLower(opts.Source))
	}

	query := "DELETE FROM torrents"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}

	// Shrink memory and checkpoint after pruning
	s.PruneMemory()

	return affected, nil
}

// CountDeadTorrents returns the number of torrents that match the dead torrent criteria
func (s *Store) CountDeadTorrents(maxSeeders int, olderThan time.Time) (int64, error) {
	var count int64
	query := "SELECT COUNT(1) FROM torrents WHERE seeders <= ?"
	args := []any{maxSeeders}
	if !olderThan.IsZero() {
		query += " AND created_time <= ?"
		args = append(args, olderThan.Format("2006-01-02 15:04:05"))
	}
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// UpdatePeerCount updates the seeders and leechers count for an existing torrent and bumps last_seen
func (s *Store) UpdatePeerCount(infoHash string, seeders, leechers int) error {
	cleanHash := strings.ToLower(strings.TrimSpace(infoHash))
	_, err := s.db.Exec(`
		UPDATE torrents
		SET seeders = ?, leechers = ?, last_seen = CURRENT_TIMESTAMP
		WHERE LOWER(info_hash) = ?
	`, seeders, leechers, cleanHash)
	return err
}

// GetOldestTorrents retrieves up to limit torrents with the oldest last_seen for dead torrent verification
func (s *Store) GetOldestTorrents(limit int) ([]TorrentRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT info_hash, name, size, file_count, piece_length, created_time, source, seeders, leechers, category, magnet_uri
		FROM torrents
		ORDER BY last_seen ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []TorrentRecord
	for rows.Next() {
		var r TorrentRecord
		var createdStr string
		if err := rows.Scan(&r.InfoHash, &r.Name, &r.Size, &r.FileCount, &r.PieceLength, &createdStr, &r.Source, &r.Seeders, &r.Leechers, &r.Category, &r.MagnetURI); err == nil {
			r.CreatedTime = parseDBTime(createdStr)
			results = append(results, r)
		}
	}
	return results, nil
}
