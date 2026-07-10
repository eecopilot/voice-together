package db

import (
	"context"
	"database/sql"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Clip struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	SourcePath string    `json:"source_path"`
	SourceHash string    `json:"source_hash"`
	AudioPath  string    `json:"audio_path"`
	Duration   float64   `json:"duration"`
	Language   string    `json:"language"`
	Status     string    `json:"status"`
	Error      string    `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
}

type Segment struct {
	ID     int64   `json:"id"`
	ClipID string  `json:"clip_id"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Text   string  `json:"text"`
}

func Open(ctx context.Context, path string) (*Store, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)

	store := &Store{db: database}
	if err := store.init(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`CREATE TABLE IF NOT EXISTS clips (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			source_path TEXT NOT NULL,
			source_hash TEXT NOT NULL DEFAULT '',
			audio_path TEXT NOT NULL DEFAULT '',
			duration REAL NOT NULL DEFAULT 0,
			language TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS segments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			clip_id TEXT NOT NULL,
			start REAL NOT NULL,
			end REAL NOT NULL,
			text TEXT NOT NULL,
			FOREIGN KEY (clip_id) REFERENCES clips(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_segments_clip ON segments(clip_id, start)`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE clips ADD COLUMN source_hash TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_clips_source_hash ON clips(source_hash)`); err != nil {
		return err
	}
	return nil
}

func (s *Store) CreateClip(ctx context.Context, clip Clip) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO clips (id, title, source_path, source_hash, audio_path, duration, language, status, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, clip.ID, clip.Title, clip.SourcePath, clip.SourceHash, clip.AudioPath, clip.Duration, clip.Language, clip.Status, clip.Error, clip.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *Store) UpdateClipSourceHash(ctx context.Context, id string, sourceHash string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE clips
		SET source_hash = ?
		WHERE id = ?
	`, sourceHash, id)
	return err
}

func (s *Store) MarkClipProcessing(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE clips
		SET audio_path = '', duration = 0, status = 'processing', error = ''
		WHERE id = ?
	`, id)
	return err
}

func (s *Store) UpdateClipProcessed(ctx context.Context, id string, audioPath string, duration float64, language string, status string, errText string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE clips
		SET audio_path = ?, duration = ?, language = ?, status = ?, error = ?
		WHERE id = ?
	`, audioPath, duration, language, status, errText, id)
	return err
}

func (s *Store) CompleteClipProcessing(
	ctx context.Context,
	id string,
	audioPath string,
	duration float64,
	language string,
	status string,
	errText string,
	segments []Segment,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM segments WHERE clip_id = ?`, id); err != nil {
		return err
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO segments (clip_id, start, end, text) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	for _, segment := range segments {
		if _, err := statement.ExecContext(ctx, id, segment.Start, segment.End, segment.Text); err != nil {
			_ = statement.Close()
			return err
		}
	}
	if err := statement.Close(); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE clips
		SET audio_path = ?, duration = ?, language = ?, status = ?, error = ?
		WHERE id = ?
	`, audioPath, duration, language, status, errText, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) ReplaceSegments(ctx context.Context, clipID string, segments []Segment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM segments WHERE clip_id = ?`, clipID); err != nil {
		return err
	}

	statement, err := tx.PrepareContext(ctx, `INSERT INTO segments (clip_id, start, end, text) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()

	for _, segment := range segments {
		if _, err := statement.ExecContext(ctx, clipID, segment.Start, segment.End, segment.Text); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) ListClips(ctx context.Context) ([]Clip, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, source_path, source_hash, audio_path, duration, language, status, error, created_at
		FROM clips
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clips := []Clip{}
	for rows.Next() {
		clip, err := scanClip(rows)
		if err != nil {
			return nil, err
		}
		clips = append(clips, clip)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return clips, nil
}

func (s *Store) GetClipBySourceHash(ctx context.Context, sourceHash string) (Clip, []Segment, error) {
	if sourceHash == "" {
		return Clip{}, nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, source_path, source_hash, audio_path, duration, language, status, error, created_at
		FROM clips
		WHERE source_hash = ?
		ORDER BY
			CASE status WHEN 'ready' THEN 0 WHEN 'processing' THEN 1 ELSE 2 END,
			created_at DESC
		LIMIT 1
	`, sourceHash)
	clip, err := scanClip(row)
	if err != nil {
		return Clip{}, nil, err
	}

	segments, err := s.ListSegments(ctx, clip.ID)
	if err != nil {
		return Clip{}, nil, err
	}
	return clip, segments, nil
}

func (s *Store) GetClip(ctx context.Context, id string) (Clip, []Segment, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, source_path, source_hash, audio_path, duration, language, status, error, created_at
		FROM clips
		WHERE id = ?
	`, id)
	clip, err := scanClip(row)
	if err != nil {
		return Clip{}, nil, err
	}

	segments, err := s.ListSegments(ctx, id)
	if err != nil {
		return Clip{}, nil, err
	}
	return clip, segments, nil
}

func (s *Store) ListSegments(ctx context.Context, clipID string) ([]Segment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, clip_id, start, end, text
		FROM segments
		WHERE clip_id = ?
		ORDER BY start ASC
	`, clipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := []Segment{}
	for rows.Next() {
		var segment Segment
		if err := rows.Scan(&segment.ID, &segment.ClipID, &segment.Start, &segment.End, &segment.Text); err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return segments, nil
}

func (s *Store) DeleteClip(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM segments WHERE clip_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM clips WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanClip(row scanner) (Clip, error) {
	var clip Clip
	var createdAt string
	if err := row.Scan(
		&clip.ID,
		&clip.Title,
		&clip.SourcePath,
		&clip.SourceHash,
		&clip.AudioPath,
		&clip.Duration,
		&clip.Language,
		&clip.Status,
		&clip.Error,
		&createdAt,
	); err != nil {
		return Clip{}, err
	}
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return Clip{}, err
	}
	clip.CreatedAt = parsed
	return clip, nil
}
