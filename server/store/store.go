package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"simplenvr/server/models"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY, username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL, role TEXT NOT NULL, created_at DATETIME NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, ip TEXT, port INTEGER,
			username TEXT, password TEXT, source TEXT NOT NULL, model TEXT,
			online INTEGER NOT NULL DEFAULT 0, rtsp_url TEXT, created DATETIME NOT NULL,
			record_enabled INTEGER NOT NULL DEFAULT 1, record_mode TEXT NOT NULL DEFAULT 'continuous',
			schedule_start TEXT NOT NULL DEFAULT '08:00', schedule_end TEXT NOT NULL DEFAULT '20:00',
			ai_enabled INTEGER, streams TEXT, preview_stream TEXT, record_stream TEXT)`,
		`CREATE TABLE IF NOT EXISTS segments (
			id TEXT PRIMARY KEY, device_id TEXT NOT NULL, start DATETIME NOT NULL,
			end DATETIME NOT NULL, path TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY, device_id TEXT NOT NULL, device_name TEXT,
			type TEXT NOT NULL, label TEXT, description TEXT,
			time DATETIME NOT NULL, snapshot TEXT, gif TEXT,
			video_start DATETIME, video_end DATETIME)`,
		`CREATE INDEX IF NOT EXISTS idx_segments_device ON segments(device_id, start)`,
		`CREATE INDEX IF NOT EXISTS idx_events_device_time ON events(device_id, time)`,
		`CREATE INDEX IF NOT EXISTS idx_events_time ON events(time)`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return err
		}
	}
	// Add new device columns on existing databases (idempotent-ish: ignore
	// "duplicate column" errors).
	alters := []string{
		`ALTER TABLE devices ADD COLUMN record_enabled INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE devices ADD COLUMN record_mode TEXT NOT NULL DEFAULT 'continuous'`,
		`ALTER TABLE devices ADD COLUMN schedule_start TEXT NOT NULL DEFAULT '08:00'`,
		`ALTER TABLE devices ADD COLUMN schedule_end TEXT NOT NULL DEFAULT '20:00'`,
		`ALTER TABLE devices ADD COLUMN ai_enabled INTEGER`,
		`ALTER TABLE devices ADD COLUMN streams TEXT`,
		`ALTER TABLE devices ADD COLUMN preview_stream TEXT`,
		`ALTER TABLE devices ADD COLUMN record_stream TEXT`,
	}
	for _, a := range alters {
		if _, err := s.db.Exec(a); err != nil {
			// column already exists — fine
		}
	}
	return nil
}

// ---- users ----

func (s *Store) CreateUser(u models.User) error {
	_, err := s.db.Exec(
		`INSERT INTO users(id, username, password_hash, role, created_at) VALUES(?,?,?,?,?)`,
		u.ID, u.Username, u.PasswordHash, string(u.Role), u.CreatedAt)
	return err
}

func (s *Store) GetUserByName(name string) (*models.User, error) {
	row := s.db.QueryRow(`SELECT id, username, password_hash, role, created_at FROM users WHERE username=?`, name)
	return scanUser(row)
}

func (s *Store) GetUserByID(id string) (*models.User, error) {
	row := s.db.QueryRow(`SELECT id, username, password_hash, role, created_at FROM users WHERE id=?`, id)
	return scanUser(row)
}

func (s *Store) ListUsers() ([]models.User, error) {
	rows, err := s.db.Query(`SELECT id, username, password_hash, role, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		u := models.User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUserPassword(id, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (s *Store) UpdateUserRole(id string, role models.Role) error {
	_, err := s.db.Exec(`UPDATE users SET role=? WHERE id=?`, string(role), id)
	return err
}

func (s *Store) DeleteUser(id string) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *Store) CountAdmins() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role=?`, string(models.RoleAdmin)).Scan(&n)
	return n, err
}

func scanUser(row *sql.Row) (*models.User, error) {
	u := models.User{}
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ---- devices ----

func (s *Store) ListDevices() ([]models.Device, error) {
	rows, err := s.db.Query(`SELECT id, name, ip, port, username, password, source, model, online, rtsp_url, created,
		record_enabled, record_mode, schedule_start, schedule_end, ai_enabled, streams, preview_stream, record_stream
		FROM devices ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []models.Device{}
	}
	return out, nil
}

func (s *Store) GetDevice(id string) (*models.Device, error) {
	row := s.db.QueryRow(`SELECT id, name, ip, port, username, password, source, model, online, rtsp_url, created,
		record_enabled, record_mode, schedule_start, schedule_end, ai_enabled, streams, preview_stream, record_stream
		FROM devices WHERE id=?`, id)
	d, err := scanDeviceRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

func scanDevice(rows *sql.Rows) (*models.Device, error) {
	return scanDeviceRow(&rowAdapter{rows})
}

type rowAdapter struct{ *sql.Rows }

func (r *rowAdapter) Scan(dest ...any) error { return r.Rows.Scan(dest...) }

func scanDeviceRow(scanner interface {
	Scan(dest ...any) error
}) (*models.Device, error) {
	d := models.Device{}
	var online int
	var recEnabled int
	var aiEnabled sql.NullInt64
	var streamsJSON sql.NullString
	var previewStream sql.NullString
	var recordStream sql.NullString
	err := scanner.Scan(&d.ID, &d.Name, &d.IP, &d.Port, &d.Username, &d.Password, &d.Source, &d.Model, &online, &d.RTSPURL, &d.Created,
		&recEnabled, &d.RecordMode, &d.ScheduleStart, &d.ScheduleEnd, &aiEnabled, &streamsJSON, &previewStream, &recordStream)
	if err != nil {
		return nil, err
	}
	d.Online = online == 1
	d.RecordEnabled = recEnabled == 1
	if aiEnabled.Valid {
		v := aiEnabled.Int64 == 1
		d.AIEnabled = &v
	}
	if streamsJSON.Valid && streamsJSON.String != "" {
		_ = json.Unmarshal([]byte(streamsJSON.String), &d.Streams)
	}
	if previewStream.Valid {
		d.PreviewStream = previewStream.String
	}
	if recordStream.Valid {
		d.RecordStream = recordStream.String
	}
	return &d, nil
}

func (s *Store) CreateDevice(d models.Device) error {
	online := 0
	if d.Online {
		online = 1
	}
	recEnabled := 1
	if !d.RecordEnabled {
		recEnabled = 0
	}
	streamsJSON, _ := json.Marshal(d.Streams)
	_, err := s.db.Exec(`INSERT INTO devices(id, name, ip, port, username, password, source, model, online, rtsp_url, created,
		record_enabled, record_mode, schedule_start, schedule_end, ai_enabled, streams, preview_stream, record_stream)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Name, d.IP, d.Port, d.Username, d.Password, string(d.Source), d.Model, online, d.RTSPURL, d.Created,
		recEnabled, d.RecordMode, d.ScheduleStart, d.ScheduleEnd, aiNull(d.AIEnabled), string(streamsJSON), d.PreviewStream, d.RecordStream)
	return err
}

func (s *Store) UpdateDevice(d models.Device) error {
	online := 0
	if d.Online {
		online = 1
	}
	recEnabled := 1
	if !d.RecordEnabled {
		recEnabled = 0
	}
	streamsJSON, _ := json.Marshal(d.Streams)
	_, err := s.db.Exec(`UPDATE devices SET name=?, ip=?, port=?, username=?, password=?, source=?, model=?, online=?, rtsp_url=?,
		record_enabled=?, record_mode=?, schedule_start=?, schedule_end=?, ai_enabled=?, streams=?, preview_stream=?, record_stream=?
		WHERE id=?`,
		d.Name, d.IP, d.Port, d.Username, d.Password, string(d.Source), d.Model, online, d.RTSPURL,
		recEnabled, d.RecordMode, d.ScheduleStart, d.ScheduleEnd, aiNull(d.AIEnabled), string(streamsJSON), d.PreviewStream, d.RecordStream, d.ID)
	return err
}

func aiNull(b *bool) any {
	if b == nil {
		return nil
	}
	if *b {
		return 1
	}
	return 0
}

func (s *Store) SetDeviceOnline(id string, online bool) error {
	v := 0
	if online {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE devices SET online=? WHERE id=?`, v, id)
	return err
}

func (s *Store) DeleteDevice(id string) error {
	if _, err := s.db.Exec(`DELETE FROM devices WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM segments WHERE device_id=?`, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM events WHERE device_id=?`, id); err != nil {
		return err
	}
	return nil
}

// ---- segments ----

func (s *Store) UpsertSegment(seg models.RecordingSegment) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO segments(id, device_id, start, end, path) VALUES(?,?,?,?,?)`,
		seg.ID, seg.DeviceID, seg.Start, seg.End, seg.Path)
	return err
}

func (s *Store) SegmentsForDay(deviceID string, dayStart, dayEnd time.Time) ([]models.RecordingSegment, error) {
	rows, err := s.db.Query(`SELECT id, device_id, start, end, path FROM segments
		WHERE device_id=? AND start < ? AND end > ? ORDER BY start`,
		deviceID, dayEnd, dayStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSegments(rows)
}

func (s *Store) SegmentsAfter(deviceID string, start time.Time) ([]models.RecordingSegment, error) {
	rows, err := s.db.Query(`SELECT id, device_id, start, end, path FROM segments
		WHERE device_id=? AND end > ? ORDER BY start`, deviceID, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSegments(rows)
}

func (s *Store) SegmentsCountOnDay(deviceID string, dayStart, dayEnd time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM segments WHERE device_id=? AND start < ? AND end > ?`,
		deviceID, dayEnd, dayStart).Scan(&n)
	return n, err
}

func scanSegments(rows *sql.Rows) ([]models.RecordingSegment, error) {
	var out []models.RecordingSegment
	for rows.Next() {
		seg := models.RecordingSegment{}
		if err := rows.Scan(&seg.ID, &seg.DeviceID, &seg.Start, &seg.End, &seg.Path); err != nil {
			return nil, err
		}
		out = append(out, seg)
	}
	return out, rows.Err()
}

// ---- events ----

func (s *Store) CreateEvent(e models.Event) error {
	var vs, ve any
	if e.VideoStart != nil {
		vs = *e.VideoStart
	}
	if e.VideoEnd != nil {
		ve = *e.VideoEnd
	}
	_, err := s.db.Exec(`INSERT INTO events(id, device_id, device_name, type, label, description, time, snapshot, gif, video_start, video_end)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.DeviceID, e.DeviceName, string(e.Type), e.Label, e.Desc, e.Time, e.Snapshot, e.GIF, vs, ve)
	return err
}

func (s *Store) ListEvents(deviceID, eventType string, dayStart, dayEnd *time.Time, limit, offset int) ([]models.Event, error) {
	q := `SELECT id, device_id, device_name, type, label, description, time, snapshot, gif, video_start, video_end FROM events WHERE 1=1`
	var args []any
	if deviceID != "" {
		q += ` AND device_id=?`
		args = append(args, deviceID)
	}
	if eventType != "" {
		q += ` AND type=?`
		args = append(args, eventType)
	}
	if dayStart != nil {
		q += ` AND time >= ?`
		args = append(args, *dayStart)
	}
	if dayEnd != nil {
		q += ` AND time < ?`
		args = append(args, *dayEnd)
	}
	if limit <= 0 {
		limit = 100
	}
	q += ` ORDER BY time DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evs, err := scanEvents(rows)
	if err != nil {
		return nil, err
	}
	if evs == nil {
		evs = []models.Event{}
	}
	return evs, nil
}

// CountEvents returns the total number of matching events (same filters as ListEvents).
func (s *Store) CountEvents(deviceID, eventType string, dayStart, dayEnd *time.Time) (int, error) {
	q := `SELECT COUNT(*) FROM events WHERE 1=1`
	var args []any
	if deviceID != "" {
		q += ` AND device_id=?`
		args = append(args, deviceID)
	}
	if eventType != "" {
		q += ` AND type=?`
		args = append(args, eventType)
	}
	if dayStart != nil {
		q += ` AND time >= ?`
		args = append(args, *dayStart)
	}
	if dayEnd != nil {
		q += ` AND time < ?`
		args = append(args, *dayEnd)
	}
	var n int
	err := s.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

func scanEvents(rows *sql.Rows) ([]models.Event, error) {
	var out []models.Event
	for rows.Next() {
		e := models.Event{}
		var vs, ve sql.NullTime
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.DeviceName, &e.Type, &e.Label, &e.Desc, &e.Time, &e.Snapshot, &e.GIF, &vs, &ve); err != nil {
			return nil, err
		}
		if vs.Valid {
			t := vs.Time
			e.VideoStart = &t
		}
		if ve.Valid {
			t := ve.Time
			e.VideoEnd = &t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeleteEventsBefore(t time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM events WHERE time < ?`, t)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) GetEvent(id string) (*models.Event, error) {
	row := s.db.QueryRow(`SELECT id, device_id, device_name, type, label, description, time, snapshot, gif, video_start, video_end FROM events WHERE id=?`, id)
	e := models.Event{}
	var vs, ve sql.NullTime
	err := row.Scan(&e.ID, &e.DeviceID, &e.DeviceName, &e.Type, &e.Label, &e.Desc, &e.Time, &e.Snapshot, &e.GIF, &vs, &ve)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if vs.Valid {
		t := vs.Time
		e.VideoStart = &t
	}
	if ve.Valid {
		t := ve.Time
		e.VideoEnd = &t
	}
	return &e, nil
}

func (s *Store) DeleteEvent(id string) error {
	_, err := s.db.Exec(`DELETE FROM events WHERE id=?`, id)
	return err
}
