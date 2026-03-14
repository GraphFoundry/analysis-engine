package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DecisionStore persists simulation decisions in SQLite.
type DecisionStore struct {
	db *sql.DB
}

var ErrWebhookEventHashConflict = errors.New("webhook event hash conflict")

// NewDecisionStore initializes the SQLite database and ensures schema availability.
func NewDecisionStore(dbPath string) (*DecisionStore, error) {

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	store := &DecisionStore{db: db}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *DecisionStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS decisions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		type TEXT NOT NULL,
		scenario TEXT NOT NULL,
		result TEXT NOT NULL,
		correlation_id TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_decisions_timestamp ON decisions(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_decisions_type ON decisions(type);
	CREATE INDEX IF NOT EXISTS idx_decisions_correlation_id ON decisions(correlation_id);

	CREATE TABLE IF NOT EXISTS webhook_events (
		event_id TEXT PRIMARY KEY,
		event_hash TEXT NOT NULL,
		source TEXT,
		correlation_id TEXT,
		event_type TEXT,
		first_seen_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_webhook_events_first_seen_at ON webhook_events(first_seen_at);
	CREATE INDEX IF NOT EXISTS idx_webhook_events_source ON webhook_events(source);

	CREATE TABLE IF NOT EXISTS drill_runs (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		target TEXT NOT NULL,
		status TEXT NOT NULL,
		start_time TEXT NOT NULL,
		end_time TEXT,
		config TEXT NOT NULL,
		pre_snapshot TEXT,
		post_snapshot TEXT,
		verdict TEXT,
		scenario_id TEXT,
		validation_status TEXT,
		rollback_verified_at TEXT,
		rollback_verification_source TEXT,
		banner_verified INTEGER,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS drill_steps (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id TEXT NOT NULL,
		timestamp TEXT NOT NULL,
		phase TEXT NOT NULL,
		message TEXT NOT NULL,
		status TEXT NOT NULL,
		FOREIGN KEY (run_id) REFERENCES drill_runs(id) ON DELETE CASCADE
	);
	
	CREATE INDEX IF NOT EXISTS idx_drill_runs_status ON drill_runs(status);
	CREATE INDEX IF NOT EXISTS idx_drill_steps_run_id ON drill_steps(run_id);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to init schema: %w", err)
	}

	if err := s.migrateDrillRunValidationColumns(); err != nil {
		return err
	}
	return nil
}

func (s *DecisionStore) migrateDrillRunValidationColumns() error {
	columns, err := s.getTableColumnSet("drill_runs")
	if err != nil {
		return fmt.Errorf("failed to inspect drill_runs columns for migration: %w", err)
	}

	type columnMigration struct {
		name       string
		definition string
	}

	migrations := []columnMigration{
		{name: "scenario_id", definition: "TEXT"},
		{name: "validation_status", definition: "TEXT"},
		{name: "rollback_verified_at", definition: "TEXT"},
		{name: "rollback_verification_source", definition: "TEXT"},
		{name: "banner_verified", definition: "INTEGER"},
	}

	for _, migration := range migrations {
		if _, exists := columns[migration.name]; exists {
			continue
		}

		statement := fmt.Sprintf(
			"ALTER TABLE drill_runs ADD COLUMN %s %s",
			migration.name,
			migration.definition,
		)
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("failed to apply migration for drill_runs.%s: %w", migration.name, err)
		}
	}

	return nil
}

func (s *DecisionStore) getTableColumnSet(tableName string) (map[string]struct{}, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var defaultValue sql.NullString
		var pk int

		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return columns, nil
}

// Close closes the underlying SQLite connection.
func (s *DecisionStore) Close() error {
	return s.db.Close()
}

// LogDecisionInput is the payload required to insert a decision record.
type LogDecisionInput struct {
	Timestamp     string      `json:"timestamp"`
	Type          string      `json:"type"`
	Scenario      interface{} `json:"scenario"`
	Result        interface{} `json:"result"`
	CorrelationID string      `json:"correlationId"`
}

// DecisionRecord is the stored representation returned by decision queries.
type DecisionRecord struct {
	ID            int64       `json:"id"`
	Timestamp     string      `json:"timestamp"`
	Type          string      `json:"type"`
	Scenario      interface{} `json:"scenario"`
	Result        interface{} `json:"result"`
	CorrelationID string      `json:"correlationId"`
	CreatedAt     string      `json:"createdAt"`
}

// LogDecision inserts one decision record and returns the inserted metadata.
func (s *DecisionStore) LogDecision(input LogDecisionInput) (*DecisionRecord, error) {
	scenarioJSON, err := json.Marshal(input.Scenario)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal scenario: %w", err)
	}
	resultJSON, err := json.Marshal(input.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	query := `
		INSERT INTO decisions (timestamp, type, scenario, result, correlation_id)
		VALUES (?, ?, ?, ?, ?)
	`
	res, err := s.db.Exec(query, input.Timestamp, input.Type, string(scenarioJSON), string(resultJSON), input.CorrelationID)
	if err != nil {
		return nil, fmt.Errorf("failed to insert decision: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return &DecisionRecord{
		ID:            id,
		Timestamp:     input.Timestamp,
		Type:          input.Type,
		Scenario:      input.Scenario,
		Result:        input.Result,
		CorrelationID: input.CorrelationID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// GetHistoryOptions defines filters and pagination for history reads.
type GetHistoryOptions struct {
	Limit  int
	Offset int
	Type   string
}

// GetHistory returns decision history ordered by timestamp descending.
func (s *DecisionStore) GetHistory(opts GetHistoryOptions) ([]DecisionRecord, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	query := "SELECT id, timestamp, type, scenario, result, correlation_id, created_at FROM decisions"
	args := []interface{}{}

	if opts.Type != "" {
		query += " WHERE type = ?"
		args = append(args, opts.Type)
	}

	query += " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query decisions: %w", err)
	}
	defer rows.Close()

	var records []DecisionRecord
	for rows.Next() {
		r, err := scanDecisionRow(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}

	return records, nil
}

// GetByID returns a single decision by ID.
func (s *DecisionStore) GetByID(id int64) (*DecisionRecord, error) {
	query := "SELECT id, timestamp, type, scenario, result, correlation_id, created_at FROM decisions WHERE id = ? LIMIT 1"
	rows, err := s.db.Query(query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query decision by id: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	record, err := scanDecisionRow(rows)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// GetHistorySince returns all decision records from a timestamp (inclusive), ordered by timestamp ascending.
func (s *DecisionStore) GetHistorySince(since time.Time) ([]DecisionRecord, error) {
	query := "SELECT id, timestamp, type, scenario, result, correlation_id, created_at FROM decisions WHERE timestamp >= ? ORDER BY timestamp ASC"
	rows, err := s.db.Query(query, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to query decisions since: %w", err)
	}
	defer rows.Close()

	records := []DecisionRecord{}
	for rows.Next() {
		r, err := scanDecisionRow(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// GetCount returns the number of decisions, optionally filtered by type.
func (s *DecisionStore) GetCount(decisionType string) (int, error) {
	query := "SELECT COUNT(*) FROM decisions"
	args := []interface{}{}
	if decisionType != "" {
		query += " WHERE type = ?"
		args = append(args, decisionType)
	}

	var count int
	err := s.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count decisions: %w", err)
	}
	return count, nil
}

func scanDecisionRow(rows *sql.Rows) (DecisionRecord, error) {
	var record DecisionRecord
	var scenarioStr, resultStr string
	var corrID sql.NullString

	if err := rows.Scan(&record.ID, &record.Timestamp, &record.Type, &scenarioStr, &resultStr, &corrID, &record.CreatedAt); err != nil {
		return DecisionRecord{}, fmt.Errorf("failed to scan row: %w", err)
	}
	if corrID.Valid {
		record.CorrelationID = corrID.String
	}
	if err := json.Unmarshal([]byte(scenarioStr), &record.Scenario); err != nil {
		return DecisionRecord{}, fmt.Errorf("failed to unmarshal scenario: %w", err)
	}
	if err := json.Unmarshal([]byte(resultStr), &record.Result); err != nil {
		return DecisionRecord{}, fmt.Errorf("failed to unmarshal result: %w", err)
	}
	return record, nil
}

// RegisterWebhookEvent stores a webhook event ID for idempotency and returns whether it is a duplicate.
// Old rows outside the dedupe window are pruned during registration.
func (s *DecisionStore) RegisterWebhookEvent(
	eventID, eventHash, source, correlationID, eventType string,
	dedupeWindow time.Duration,
) (bool, error) {
	if eventID == "" {
		return false, fmt.Errorf("event ID is required")
	}
	if eventHash == "" {
		return false, fmt.Errorf("event hash is required")
	}
	if dedupeWindow <= 0 {
		dedupeWindow = 24 * time.Hour
	}

	now := time.Now().UTC()
	firstSeenAt := now.Format(time.RFC3339)
	cutoff := now.Add(-dedupeWindow).Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("failed to begin webhook registration tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(`DELETE FROM webhook_events WHERE first_seen_at < ?`, cutoff); err != nil {
		return false, fmt.Errorf("failed to prune webhook dedupe window: %w", err)
	}

	res, err := tx.Exec(
		`INSERT OR IGNORE INTO webhook_events (event_id, event_hash, source, correlation_id, event_type, first_seen_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		eventID, eventHash, source, correlationID, eventType, firstSeenAt,
	)
	if err != nil {
		return false, fmt.Errorf("failed to insert webhook event: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect webhook registration result: %w", err)
	}

	isDuplicate := rowsAffected == 0
	if isDuplicate {
		var existingHash string
		if err := tx.QueryRow(
			`SELECT event_hash FROM webhook_events WHERE event_id = ?`,
			eventID,
		).Scan(&existingHash); err != nil {
			return false, fmt.Errorf("failed to load existing webhook event hash: %w", err)
		}
		if existingHash != eventHash {
			if err := tx.Commit(); err != nil {
				return false, fmt.Errorf("failed to commit hash-conflict webhook tx: %w", err)
			}
			return true, ErrWebhookEventHashConflict
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit webhook registration tx: %w", err)
	}

	return isDuplicate, nil
}
