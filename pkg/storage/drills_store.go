package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// DrillRun represents a saved drill execution sequence.
type DrillRun struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Target       string          `json:"target"`
	Status       string          `json:"status"`
	StartTime    string          `json:"startTime"`
	EndTime      *string         `json:"endTime,omitempty"`
	Config       json.RawMessage `json:"config"`
	PreSnapshot  json.RawMessage `json:"preSnapshot,omitempty"`
	PostSnapshot json.RawMessage `json:"postSnapshot,omitempty"`
	Verdict      string          `json:"verdict"`
	CreatedAt    string          `json:"createdAt"`
	Timeline     []DrillStep     `json:"timeline"`
}

// DrillStep is a single log entry or phase transition for a drill.
type DrillStep struct {
	ID        int64  `json:"id,omitempty"`
	RunID     string `json:"runId"`
	Timestamp string `json:"timestamp"`
	Phase     string `json:"phase"`
	Message   string `json:"message"`
	Status    string `json:"status"` // "Ok", "Error"
}

// InsertDrillRun creates a new record for a drill run.
func (s *DecisionStore) InsertDrillRun(run DrillRun) error {
	configStr := "{}"
	if run.Config != nil {
		configStr = string(run.Config)
	}

	query := `
		INSERT INTO drill_runs (id, type, target, status, start_time, config, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query, run.ID, run.Type, run.Target, run.Status, run.StartTime, configStr, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to insert drill run: %w", err)
	}
	return nil
}

// UpdateDrillRun updates an existing drill run.
func (s *DecisionStore) UpdateDrillRun(run DrillRun) error {
	configStr := "{}"
	if run.Config != nil {
		configStr = string(run.Config)
	}
	var preStr, postStr *string

	if run.PreSnapshot != nil {
		str := string(run.PreSnapshot)
		preStr = &str
	}
	if run.PostSnapshot != nil {
		str := string(run.PostSnapshot)
		postStr = &str
	}

	query := `
		UPDATE drill_runs 
		SET status = ?, end_time = ?, config = ?, pre_snapshot = ?, post_snapshot = ?, verdict = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query, run.Status, run.EndTime, configStr, preStr, postStr, run.Verdict, run.ID)
	if err != nil {
		return fmt.Errorf("failed to update drill run: %w", err)
	}
	return nil
}

// AddDrillStep logs a step in the drill timeline.
func (s *DecisionStore) AddDrillStep(step DrillStep) error {
	query := `
		INSERT INTO drill_steps (run_id, timestamp, phase, message, status)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query, step.RunID, step.Timestamp, step.Phase, step.Message, step.Status)
	if err != nil {
		return fmt.Errorf("failed to insert drill step: %w", err)
	}
	return nil
}

// GetDrillRun retrieves a drill run with its timeline.
func (s *DecisionStore) GetDrillRun(id string) (*DrillRun, error) {
	query := `
		SELECT id, type, target, status, start_time, end_time, config, pre_snapshot, post_snapshot, verdict, created_at 
		FROM drill_runs WHERE id = ?
	`
	row := s.db.QueryRow(query, id)

	var run DrillRun
	var configStr string
	var preStr, postStr sql.NullString
	var endTime sql.NullString

	var verdictStr sql.NullString

	err := row.Scan(&run.ID, &run.Type, &run.Target, &run.Status, &run.StartTime, &endTime, &configStr, &preStr, &postStr, &verdictStr, &run.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to scan drill run: %w", err)
	}

	if verdictStr.Valid {
		run.Verdict = verdictStr.String
	}

	if endTime.Valid {
		run.EndTime = &endTime.String
	}
	if configStr != "" {
		run.Config = json.RawMessage(configStr)
	}
	if preStr.Valid && preStr.String != "" {
		run.PreSnapshot = json.RawMessage(preStr.String)
	}
	if postStr.Valid && postStr.String != "" {
		run.PostSnapshot = json.RawMessage(postStr.String)
	}

	// Fetch timeline
	timelineQuery := `SELECT id, run_id, timestamp, phase, message, status FROM drill_steps WHERE run_id = ? ORDER BY timestamp ASC, id ASC`
	rows, err := s.db.Query(timelineQuery, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query drill steps: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var step DrillStep
		if err := rows.Scan(&step.ID, &step.RunID, &step.Timestamp, &step.Phase, &step.Message, &step.Status); err != nil {
			return nil, fmt.Errorf("failed to scan drill step: %w", err)
		}
		run.Timeline = append(run.Timeline, step)
	}

	return &run, nil
}

// ListDrillRuns retrieves recent drill runs.
func (s *DecisionStore) ListDrillRuns(limit int) ([]DrillRun, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, type, target, status, start_time, end_time, config, verdict, created_at 
		FROM drill_runs 
		ORDER BY start_time DESC LIMIT ?
	`
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list drill runs: %w", err)
	}
	defer rows.Close()

	var runs []DrillRun
	for rows.Next() {
		var run DrillRun
		var configStr string
		var verdictStr sql.NullString
		var endTime sql.NullString

		if err := rows.Scan(&run.ID, &run.Type, &run.Target, &run.Status, &run.StartTime, &endTime, &configStr, &verdictStr, &run.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan drill run list: %w", err)
		}
		if verdictStr.Valid {
			run.Verdict = verdictStr.String
		}
		if endTime.Valid {
			run.EndTime = &endTime.String
		}
		if configStr != "" {
			run.Config = json.RawMessage(configStr)
		}
		runs = append(runs, run)
	}
	return runs, nil
}
