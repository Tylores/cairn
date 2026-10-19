package store

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

var (
	cleaveSepRegex = regexp.MustCompile(`(?m)^\s*---\s*$`)
	citationRegex  = regexp.MustCompile(`\(([A-Z][A-Za-z\s&.\-]+,\s*\d{4}[a-z]?)\)`)
	stoneTagRegex  = regexp.MustCompile(`^<!--\s*stone:([^\s|>]+?)(?:\s*\|\s*[^>]*)?\s*-->$`)

	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(rand.Reader, 0)
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS notes (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    is_monolith INTEGER NOT NULL DEFAULT 0,
    body TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    content_hash TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
    id UNINDEXED,
    body,
    content='notes',
    content_rowid='rowid',
    tokenize='porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS notes_ai AFTER INSERT ON notes BEGIN
    INSERT INTO notes_fts(rowid, id, body) VALUES (new.rowid, new.id, new.body);
END;
CREATE TRIGGER IF NOT EXISTS notes_ad AFTER DELETE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, id, body) VALUES ('delete', old.rowid, old.id, old.body);
END;
CREATE TRIGGER IF NOT EXISTS notes_au AFTER UPDATE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, id, body) VALUES ('delete', old.rowid, old.id, old.body);
    INSERT INTO notes_fts(rowid, id, body) VALUES (new.rowid, new.id, new.body);
END;

CREATE TABLE IF NOT EXISTS trails (
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    PRIMARY KEY (source_id, target_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    name TEXT PRIMARY KEY,
    chisel_text TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS session_items (
    session_name TEXT NOT NULL,
    position INTEGER NOT NULL,
    item_type TEXT NOT NULL,
    note_id TEXT,
    header_text TEXT,
    PRIMARY KEY (session_name, position)
);
`

// Note represents an indexed Quarry stone or carved Monolith.
type Note struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	IsMonolith  bool   `json:"is_monolith"`
	Body        string `json:"body"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	ContentHash string `json:"content_hash"`
	HasTrail    bool   `json:"has_trail"`
	HasCitation bool   `json:"has_citation"`
}

// SessionItem represents an ordered entry in the Cairn working stack.
type SessionItem struct {
	Position   int    `json:"position"`
	ItemType   string `json:"item_type"` // "stone" or "header"
	NoteID     string `json:"note_id,omitempty"`
	HeaderText string `json:"header_text,omitempty"`
	Body       string `json:"body,omitempty"`
	IsMonolith bool   `json:"is_monolith,omitempty"`
}

// Session represents an active Cairn trail workbench session.
type Session struct {
	Name       string        `json:"name"`
	ChiselText string        `json:"chisel_text"`
	UpdatedAt  int64         `json:"updated_at"`
	Items      []SessionItem `json:"items"`
}

// Store manages the filesystem repository and derived SQLite index.
type Store struct {
	RootDir string
	DB      *sql.DB
}

// DefaultRootDir resolves the Cairn workspace root directory:
// 1. $CAIRN_DIR environment variable if set.
// 2. Current working directory (".") if ./quarry and ./monoliths exist locally.
// 3. Fallback to ~/.local/share/cairn.
func DefaultRootDir() string {
	if env := strings.TrimSpace(os.Getenv("CAIRN_DIR")); env != "" {
		return env
	}
	if isDir("quarry") && isDir("monoliths") {
		return "."
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return filepath.Join(home, ".local", "share", "cairn")
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Open initializes the directory layout and opens <RootDir>/quarry.db with WAL and FTS5 schema.
func Open(rootDir string) (*Store, error) {
	if strings.TrimSpace(rootDir) == "" {
		rootDir = DefaultRootDir()
	}

	dirs := []string{
		filepath.Join(rootDir, "quarry", ".trash"),
		filepath.Join(rootDir, "monoliths"),
		filepath.Join(rootDir, ".cairn"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", d, err)
		}
	}

	dbPath := filepath.Join(rootDir, "quarry.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize schema: %w", err)
	}

	return &Store{
		RootDir: rootDir,
		DB:      db,
	}, nil
}

// Close closes the underlying SQLite database connection.
func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

// HasCitation reports whether body contains an inline (Author, Year) citation.
func HasCitation(body string) bool {
	return citationRegex.MatchString(body)
}

func newULID() (string, error) {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	id, err := ulid.New(ulid.Timestamp(time.Now().UTC()), ulidEntropy)
	if err != nil {
		return "", fmt.Errorf("generate ulid: %w", err)
	}
	return id.String(), nil
}

// CleaveAndWrite splits raw input on standalone --- lines, writes each chunk to
// quarry/<ULID>.md, links sequential chunks via AddTrail, and optionally links
// the first created stone to tetherID.
func (s *Store) CleaveAndWrite(raw string, tetherID string) ([]string, error) {
	parts := cleaveSepRegex.Split(raw, -1)
	chunks := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			chunks = append(chunks, trimmed)
		}
	}
	if len(chunks) == 0 {
		return nil, nil
	}

	createdIDs := make([]string, 0, len(chunks))
	var prevID string
	for _, chunk := range chunks {
		id, err := newULID()
		if err != nil {
			return createdIDs, err
		}
		targetPath := filepath.Join(s.RootDir, "quarry", id+".md")
		if err := os.WriteFile(targetPath, []byte(chunk+"\n"), 0o644); err != nil {
			return createdIDs, fmt.Errorf("write stone %s: %w", targetPath, err)
		}
		createdIDs = append(createdIDs, id)
		if prevID != "" {
			if err := s.AddTrail(prevID, id); err != nil {
				return createdIDs, fmt.Errorf("link sequential stones %s -> %s: %w", prevID, id, err)
			}
		}
		prevID = id
	}

	tetherID = strings.TrimSpace(tetherID)
	if tetherID != "" && len(createdIDs) > 0 {
		if err := s.AddTrail(createdIDs[0], tetherID); err != nil {
			return createdIDs, fmt.Errorf("tether stone %s -> %s: %w", createdIDs[0], tetherID, err)
		}
	}

	return createdIDs, nil
}

// AddTrail inserts an edge into SQLite trails and appends src\tdst to trails.tsv if not present.
func (s *Store) AddTrail(src, dst string) error {
	src = strings.TrimSpace(src)
	dst = strings.TrimSpace(dst)
	if src == "" || dst == "" || src == dst {
		return nil
	}

	if _, err := s.DB.Exec(`INSERT OR IGNORE INTO trails (source_id, target_id) VALUES (?, ?)`, src, dst); err != nil {
		return fmt.Errorf("insert trail %s -> %s: %w", src, dst, err)
	}

	trailsPath := filepath.Join(s.RootDir, "trails.tsv")
	existing, err := os.ReadFile(trailsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read trails.tsv: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(existing)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == src && fields[1] == dst {
			return nil
		}
	}

	var buf strings.Builder
	if len(existing) > 0 {
		buf.Write(existing)
		if existing[len(existing)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	buf.WriteString(src)
	buf.WriteByte('\t')
	buf.WriteString(dst)
	buf.WriteByte('\n')

	if err := os.WriteFile(trailsPath, []byte(buf.String()), 0o644); err != nil {
		return fmt.Errorf("write trails.tsv: %w", err)
	}
	return nil
}

// Erode moves a raw quarry stone to quarry/.trash/<id>.md and deletes its references.
// Monolith IDs ("monolith:...") are rejected.
func (s *Store) Erode(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("stone id cannot be empty")
	}
	if strings.HasPrefix(id, "monolith:") {
		return fmt.Errorf("cannot erode permanent monolith %q", id)
	}

	srcPath := filepath.Join(s.RootDir, "quarry", id+".md")
	dstDir := filepath.Join(s.RootDir, "quarry", ".trash")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("ensure trash directory: %w", err)
	}
	dstPath := filepath.Join(dstDir, id+".md")

	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("move stone %s to trash: %w", id, err)
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin erode tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM trails WHERE source_id = ? OR target_id = ?`, id, id); err != nil {
		return fmt.Errorf("delete trails for %s: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM session_items WHERE note_id = ?`, id); err != nil {
		return fmt.Errorf("delete session items for %s: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM notes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete note %s: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit erode tx: %w", err)
	}

	_ = s.removeFromTrailsTSV(id)
	return nil
}

func (s *Store) removeFromTrailsTSV(id string) error {
	trailsPath := filepath.Join(s.RootDir, "trails.tsv")
	raw, err := os.ReadFile(trailsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var kept []string
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == id || fields[1] == id) {
			continue
		}
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	out := ""
	if len(kept) > 0 {
		out = strings.Join(kept, "\n") + "\n"
	}
	return os.WriteFile(trailsPath, []byte(out), 0o644)
}

// SyncIndex synchronizes quarry/*.md, monoliths/*.md, and trails.tsv with quarry.db.
// It returns the total count of indexed Markdown files on disk.
func (s *Store) SyncIndex(rebuild bool) (int, error) {
	type diskFile struct {
		id          string
		relPath     string
		isMonolith  int
		body        string
		createdAt   int64
		updatedAt   int64
		contentHash string
	}

	diskNotes := make(map[string]diskFile)

	scanDir := func(subDir string, isMonolith bool) error {
		absDir := filepath.Join(s.RootDir, subDir)
		entries, err := os.ReadDir(absDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("read directory %s: %w", absDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			fullPath := filepath.Join(absDir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("stat %s: %w", fullPath, err)
			}
			rawBytes, err := os.ReadFile(fullPath)
			if err != nil {
				return fmt.Errorf("read %s: %w", fullPath, err)
			}
			trimmedBody := strings.TrimSpace(string(rawBytes))
			sum := sha256.Sum256([]byte(trimmedBody))
			hashHex := hex.EncodeToString(sum[:])

			base := strings.TrimSuffix(entry.Name(), ".md")
			id := base
			monoFlag := 0
			createdAt := info.ModTime().Unix()
			if isMonolith {
				id = "monolith:" + base
				monoFlag = 1
			} else if ts, ok := parseULIDTimestampSeconds(base); ok {
				createdAt = ts
			}

			diskNotes[id] = diskFile{
				id:          id,
				relPath:     filepath.ToSlash(filepath.Join(subDir, entry.Name())),
				isMonolith:  monoFlag,
				body:        trimmedBody,
				createdAt:   createdAt,
				updatedAt:   info.ModTime().Unix(),
				contentHash: hashHex,
			}
		}
		return nil
	}

	if err := scanDir("quarry", false); err != nil {
		return 0, err
	}
	if err := scanDir("monoliths", true); err != nil {
		return 0, err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin sync tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if rebuild {
		if _, err := tx.Exec(`DELETE FROM notes`); err != nil {
			return 0, fmt.Errorf("clear notes on rebuild: %w", err)
		}
	}

	existingHashes := make(map[string]string)
	rows, err := tx.Query(`SELECT id, content_hash FROM notes`)
	if err != nil {
		return 0, fmt.Errorf("query existing notes: %w", err)
	}
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan existing note: %w", err)
		}
		existingHashes[id] = hash
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close existing notes rows: %w", err)
	}

	for existingID := range existingHashes {
		if _, stillOnDisk := diskNotes[existingID]; !stillOnDisk {
			if _, err := tx.Exec(`DELETE FROM notes WHERE id = ?`, existingID); err != nil {
				return 0, fmt.Errorf("delete missing note %s: %w", existingID, err)
			}
		}
	}

	for id, df := range diskNotes {
		prevHash, exists := existingHashes[id]
		if !exists {
			if _, err := tx.Exec(`
				INSERT INTO notes (id, path, is_monolith, body, created_at, updated_at, content_hash)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, df.id, df.relPath, df.isMonolith, df.body, df.createdAt, df.updatedAt, df.contentHash); err != nil {
				return 0, fmt.Errorf("insert note %s: %w", id, err)
			}
		} else if prevHash != df.contentHash {
			if _, err := tx.Exec(`
				UPDATE notes
				SET path = ?, is_monolith = ?, body = ?, created_at = ?, updated_at = ?, content_hash = ?
				WHERE id = ?
			`, df.relPath, df.isMonolith, df.body, df.createdAt, df.updatedAt, df.contentHash, df.id); err != nil {
				return 0, fmt.Errorf("update note %s: %w", id, err)
			}
		}
	}

	if _, err := tx.Exec(`DELETE FROM trails`); err != nil {
		return 0, fmt.Errorf("clear trails table: %w", err)
	}

	trailsPath := filepath.Join(s.RootDir, "trails.tsv")
	if rawTrails, err := os.ReadFile(trailsPath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(rawTrails)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO trails (source_id, target_id) VALUES (?, ?)`, fields[0], fields[1]); err != nil {
					return 0, fmt.Errorf("insert trail %s -> %s: %w", fields[0], fields[1], err)
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("read trails.tsv: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit sync tx: %w", err)
	}

	return len(diskNotes), nil
}

// parseULIDTimestampSeconds extracts the 48-bit millisecond timestamp from the first
// 10 Crockford Base32 characters of a 26-character ULID string.
func parseULIDTimestampSeconds(id string) (int64, bool) {
	if len(id) != 26 {
		return 0, false
	}
	// Construct a canonical 26-char string using the 10-char timestamp prefix + 16 zeros
	// so random suffix characters outside strict Crockford Base32 (e.g. 'U') never fail parsing.
	padded := strings.ToUpper(id[:10]) + "0000000000000000"
	parsed, err := ulid.Parse(padded)
	if err != nil {
		return 0, false
	}
	sec := int64(parsed.Time()) / 1000
	if sec <= 0 {
		return 0, false
	}
	return sec, true
}

// NoteFilePath returns the filesystem path for a note ID.
func (s *Store) NoteFilePath(id string) string {
	if strings.HasPrefix(id, "monolith:") {
		slug := strings.TrimPrefix(id, "monolith:")
		return filepath.Join(s.RootDir, "monoliths", slug+".md")
	}
	return filepath.Join(s.RootDir, "quarry", id+".md")
}

// GetNote fetches a single note by ID from SQLite.
func (s *Store) GetNote(id string) (Note, bool, error) {
	var n Note
	var isMono int
	var trailCount int
	err := s.DB.QueryRow(`
		SELECT n.id, n.path, n.is_monolith, n.body, n.created_at, n.updated_at, n.content_hash,
		       (SELECT COUNT(1) FROM trails t WHERE t.source_id = n.id OR t.target_id = n.id)
		FROM notes n
		WHERE n.id = ?
	`, id).Scan(&n.ID, &n.Path, &isMono, &n.Body, &n.CreatedAt, &n.UpdatedAt, &n.ContentHash, &trailCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Note{}, false, nil
		}
		return Note{}, false, fmt.Errorf("query note %s: %w", id, err)
	}
	n.IsMonolith = isMono != 0
	n.HasTrail = trailCount > 0
	n.HasCitation = HasCitation(n.Body)
	return n, true, nil
}

// ListNotes returns all indexed notes with HasTrail and HasCitation populated.
func (s *Store) ListNotes() ([]Note, error) {
	rows, err := s.DB.Query(`
		SELECT n.id, n.path, n.is_monolith, n.body, n.created_at, n.updated_at, n.content_hash,
		       (SELECT COUNT(1) FROM trails t WHERE t.source_id = n.id OR t.target_id = n.id)
		FROM notes n
		ORDER BY n.is_monolith DESC, n.created_at DESC, n.id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Note
	for rows.Next() {
		var n Note
		var isMono, trailCount int
		if err := rows.Scan(&n.ID, &n.Path, &isMono, &n.Body, &n.CreatedAt, &n.UpdatedAt, &n.ContentHash, &trailCount); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		n.IsMonolith = isMono != 0
		n.HasTrail = trailCount > 0
		n.HasCitation = HasCitation(n.Body)
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetTrailNeighbors returns all 1-hop neighbor note IDs connected to any of the given anchorIDs.
func (s *Store) GetTrailNeighbors(anchorIDs []string) ([]string, error) {
	if len(anchorIDs) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(anchorIDs))
	var out []string
	for _, a := range anchorIDs {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		rows, err := s.DB.Query(`
			SELECT CASE WHEN source_id = ? THEN target_id ELSE source_id END AS neighbor
			FROM trails
			WHERE source_id = ? OR target_id = ?
		`, a, a, a)
		if err != nil {
			return nil, fmt.Errorf("query trail neighbors for %s: %w", a, err)
		}
		for rows.Next() {
			var nb string
			if err := rows.Scan(&nb); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan neighbor: %w", err)
			}
			if !seen[nb] {
				seen[nb] = true
				out = append(out, nb)
			}
		}
		_ = rows.Close()
	}
	return out, nil
}

// SearchFTS runs an FTS5 BM25 query against notes_fts and returns matching notes in BM25 rank order.
func (s *Store) SearchFTS(ftsExpr string, limit int) ([]Note, error) {
	if strings.TrimSpace(ftsExpr) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.DB.Query(`
		SELECT n.id, n.path, n.is_monolith, n.body, n.created_at, n.updated_at, n.content_hash,
		       (SELECT COUNT(1) FROM trails t WHERE t.source_id = n.id OR t.target_id = n.id)
		FROM notes_fts f
		JOIN notes n ON n.rowid = f.rowid
		WHERE notes_fts MATCH ?
		ORDER BY bm25(notes_fts) ASC
		LIMIT ?
	`, ftsExpr, limit)
	if err != nil {
		return nil, fmt.Errorf("search fts %q: %w", ftsExpr, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Note
	for rows.Next() {
		var n Note
		var isMono, trailCount int
		if err := rows.Scan(&n.ID, &n.Path, &isMono, &n.Body, &n.CreatedAt, &n.UpdatedAt, &n.ContentHash, &trailCount); err != nil {
			return nil, fmt.Errorf("scan fts note: %w", err)
		}
		n.IsMonolith = isMono != 0
		n.HasTrail = trailCount > 0
		n.HasCitation = HasCitation(n.Body)
		out = append(out, n)
	}
	return out, rows.Err()
}

// BootstrapTrailIfNeeded imports .cairn/trail.md into SQLite if the sessions table is currently empty.
func (s *Store) BootstrapTrailIfNeeded() (string, error) {
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(1) FROM sessions`).Scan(&count); err != nil {
		return "", fmt.Errorf("count sessions: %w", err)
	}
	if count > 0 {
		return "", nil
	}

	trailPath := filepath.Join(s.RootDir, ".cairn", "trail.md")
	raw, err := os.ReadFile(trailPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read .cairn/trail.md: %w", err)
	}

	sess := s.parseTrailMarkdown(string(raw))
	if err := s.SaveSession(sess); err != nil {
		return "", fmt.Errorf("save bootstrapped session: %w", err)
	}
	return sess.Name, nil
}

func (s *Store) parseTrailMarkdown(content string) *Session {
	sess := &Session{
		Name:      "main",
		UpdatedAt: time.Now().Unix(),
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	pos := 0
	var activeStoneBody string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# Cairn:") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "# Cairn:"))
			if name != "" {
				sess.Name = name
			}
			continue
		}
		if strings.HasPrefix(line, "> **Chisel:**") {
			sess.ChiselText = strings.TrimSpace(strings.TrimPrefix(line, "> **Chisel:**"))
			continue
		}
		if m := stoneTagRegex.FindStringSubmatch(line); len(m) == 2 {
			stoneID := strings.TrimSpace(m[1])
			if stoneID != "" {
				activeStoneBody = ""
				if n, ok, err := s.GetNote(stoneID); err == nil && ok {
					activeStoneBody = n.Body
				}
				sess.Items = append(sess.Items, SessionItem{
					Position: pos,
					ItemType: "stone",
					NoteID:   stoneID,
				})
				pos++
			}
			continue
		}
		if strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "### ") {
			headerTitle := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if headerTitle != "" {
				if activeStoneBody != "" && strings.Contains(activeStoneBody, "## "+headerTitle) {
					continue
				}
				activeStoneBody = ""
				sess.Items = append(sess.Items, SessionItem{
					Position:   pos,
					ItemType:   "header",
					HeaderText: headerTitle,
				})
				pos++
			}
			continue
		}
	}

	return sess
}

// ResolveSessionName determines the active trail name:
// 1. Explicitly passed name if non-empty.
// 2. Imported session from .cairn/trail.md if sessions table was empty.
// 3. Most recently updated session in SQLite, or "main".
func (s *Store) ResolveSessionName(requested string) (string, error) {
	if _, err := s.BootstrapTrailIfNeeded(); err != nil {
		return "", err
	}
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested, nil
	}
	var latest string
	err := s.DB.QueryRow(`SELECT name FROM sessions ORDER BY updated_at DESC LIMIT 1`).Scan(&latest)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "main", nil
		}
		return "", fmt.Errorf("resolve latest session: %w", err)
	}
	return latest, nil
}

// LoadSession loads a named session and populates note bodies for stone items.
func (s *Store) LoadSession(name string) (*Session, error) {
	resolved, err := s.ResolveSessionName(name)
	if err != nil {
		return nil, err
	}

	// Clean out any ## headers that actually belonged inside a Monolith body during initial bootstrap
	sess := &Session{
		Name:      resolved,
		UpdatedAt: time.Now().Unix(),
	}

	err = s.DB.QueryRow(`SELECT chisel_text, updated_at FROM sessions WHERE name = ?`, resolved).
		Scan(&sess.ChiselText, &sess.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load session %s: %w", resolved, err)
	}

	rows, err := s.DB.Query(`
		SELECT si.position, si.item_type, COALESCE(si.note_id, ''), COALESCE(si.header_text, ''),
		       COALESCE(n.body, ''), COALESCE(n.is_monolith, 0)
		FROM session_items si
		LEFT JOIN notes n ON n.id = si.note_id
		WHERE si.session_name = ?
		ORDER BY si.position ASC
	`, resolved)
	if err != nil {
		return nil, fmt.Errorf("load session items for %s: %w", resolved, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var item SessionItem
		var isMono int
		if err := rows.Scan(&item.Position, &item.ItemType, &item.NoteID, &item.HeaderText, &item.Body, &isMono); err != nil {
			return nil, fmt.Errorf("scan session item: %w", err)
		}
		item.IsMonolith = isMono != 0
		sess.Items = append(sess.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Filter out any header items whose text matches an internal ## heading of the immediately preceding monolith
	if len(sess.Items) > 0 {
		cleaned := make([]SessionItem, 0, len(sess.Items))
		var activeMonolithBody string
		for _, it := range sess.Items {
			if it.ItemType == "stone" {
				if it.IsMonolith {
					activeMonolithBody = it.Body
				} else {
					activeMonolithBody = ""
				}
				it.Position = len(cleaned)
				cleaned = append(cleaned, it)
				continue
			}
			if it.ItemType == "header" && activeMonolithBody != "" {
				if strings.Contains(activeMonolithBody, "## "+it.HeaderText) {
					continue
				}
			}
			activeMonolithBody = ""
			it.Position = len(cleaned)
			cleaned = append(cleaned, it)
		}
		sess.Items = cleaned
	}

	return sess, nil
}

// SaveSession persists the session metadata and ordered items in SQLite.
func (s *Store) SaveSession(sess *Session) error {
	if sess == nil {
		return errors.New("cannot save nil session")
	}
	if strings.TrimSpace(sess.Name) == "" {
		sess.Name = "main"
	}
	sess.UpdatedAt = time.Now().Unix()

	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin save session tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO sessions (name, chisel_text, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			chisel_text = excluded.chisel_text,
			updated_at = excluded.updated_at
	`, sess.Name, sess.ChiselText, sess.UpdatedAt); err != nil {
		return fmt.Errorf("upsert session %s: %w", sess.Name, err)
	}

	if _, err := tx.Exec(`DELETE FROM session_items WHERE session_name = ?`, sess.Name); err != nil {
		return fmt.Errorf("clear session items for %s: %w", sess.Name, err)
	}

	for idx, item := range sess.Items {
		if _, err := tx.Exec(`
			INSERT INTO session_items (session_name, position, item_type, note_id, header_text)
			VALUES (?, ?, ?, ?, ?)
		`, sess.Name, idx, item.ItemType, item.NoteID, item.HeaderText); err != nil {
			return fmt.Errorf("insert session item %d: %w", idx, err)
		}
	}

	return tx.Commit()
}

// FormatTrailMarkdown compiles a Session into the .cairn/trail.md Markdown format.
func (s *Store) FormatTrailMarkdown(sess *Session) (string, error) {
	var b strings.Builder
	b.WriteString("# Cairn: ")
	b.WriteString(sess.Name)
	b.WriteString("\n\n")

	if strings.TrimSpace(sess.ChiselText) != "" {
		b.WriteString("> **Chisel:** ")
		b.WriteString(strings.TrimSpace(sess.ChiselText))
		b.WriteString("\n\n")
	}

	for idx, item := range sess.Items {
		switch item.ItemType {
		case "header":
			b.WriteString("## ")
			b.WriteString(strings.TrimSpace(item.HeaderText))
			b.WriteString("\n\n")
		case "stone":
			body := strings.TrimSpace(item.Body)
			if body == "" && item.NoteID != "" {
				if n, ok, err := s.GetNote(item.NoteID); err == nil && ok {
					body = strings.TrimSpace(n.Body)
				}
			}
			b.WriteString("<!-- stone:")
			b.WriteString(item.NoteID)
			b.WriteString(" -->\n")
			if body != "" {
				b.WriteString(body)
				b.WriteString("\n")
			}
			if idx < len(sess.Items)-1 {
				b.WriteString("\n")
			}
		}
	}

	return b.String(), nil
}

// WriteTrailFile compiles the session and writes it to <RootDir>/.cairn/trail.md.
func (s *Store) WriteTrailFile(sess *Session) (string, error) {
	compiled, err := s.FormatTrailMarkdown(sess)
	if err != nil {
		return "", err
	}
	trailDir := filepath.Join(s.RootDir, ".cairn")
	if err := os.MkdirAll(trailDir, 0o755); err != nil {
		return "", fmt.Errorf("create .cairn directory: %w", err)
	}
	trailPath := filepath.Join(trailDir, "trail.md")
	if err := os.WriteFile(trailPath, []byte(compiled), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", trailPath, err)
	}
	return compiled, nil
}
