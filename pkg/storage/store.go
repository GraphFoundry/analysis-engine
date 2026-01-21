package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DecisionStore struct {
	db *sql.DB
}

func NewDecisionStore(dbPath string) (*DecisionStore, error) {

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
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
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to init schema: %w", err)
	}
	return nil
}

func (s *DecisionStore) Close() error {
	return s.db.Close()
}

type LogDecisionInput struct {
	Timestamp     string      `json:"timestamp"`
	Type          string      `json:"type"`
	Scenario      interface{} `json:"scenario"`
	Result        interface{} `json:"result"`
	CorrelationID string      `json:"correlationId"`
}

type DecisionRecord struct {
	ID            int64       `json:"id"`
	Timestamp     string      `json:"timestamp"`
	Type          string      `json:"type"`
	Scenario      interface{} `json:"scenario"`
	Result        interface{} `json:"result"`
	CorrelationID string      `json:"correlationId"`
	CreatedAt     string      `json:"createdAt"`
}

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

type GetHistoryOptions struct {
	Limit  int
	Offset int
	Type   string
}

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
		var r DecisionRecord
		var scenarioStr, resultStr string
		var corrID sql.NullString

		if err := rows.Scan(&r.ID, &r.Timestamp, &r.Type, &scenarioStr, &resultStr, &corrID, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if corrID.Valid {
			r.CorrelationID = corrID.String
		}

		if err := json.Unmarshal([]byte(scenarioStr), &r.Scenario); err != nil {
			return nil, fmt.Errorf("failed to unmarshal scenario: %w", err)
		}
		if err := json.Unmarshal([]byte(resultStr), &r.Result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal result: %w", err)
		}

		records = append(records, r)
	}

	return records, nil
}

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
