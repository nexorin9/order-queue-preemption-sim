package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB is the database connection
var DB *sql.DB

// SimulationResult represents a single simulation result stored in SQLite
type SimulationResult struct {
	ID                  int64
	Timestamp           time.Time
	NewPatientRate      float64
	RecheckPatientRate float64
	NewPatientService  float64
	RecheckService     float64
	SimulationDuration int
	PreemptionEnabled  bool
	PreemptionThreshold float64
	TotalPatients      int
	NewPatients        int
	RecheckPatients    int
	AvgWaitTime        float64
	MaxWaitTime        float64
	AvgQueueLength     float64
	MaxQueueLength     int
	ServerUtilization  float64
	NewPatientAvgWait  float64
	RecheckPatientAvgWait float64
	PreemptionCount    int
	NewPatientPreempted int
	RecheckPatientPreempted int
	Seed               int64
}

// InitDB initializes the SQLite database and creates tables if they don't exist
func InitDB(dbPath string) error {
	var err error
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	// Create tables if they don't exist
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS simulation_results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		new_patient_rate REAL NOT NULL,
		recheck_patient_rate REAL NOT NULL,
		new_patient_service REAL NOT NULL,
		recheck_service REAL NOT NULL,
		simulation_duration INTEGER NOT NULL,
		preemption_enabled INTEGER NOT NULL,
		preemption_threshold REAL NOT NULL,
		total_patients INTEGER NOT NULL,
		new_patients INTEGER NOT NULL,
		recheck_patients INTEGER NOT NULL,
		avg_wait_time REAL NOT NULL,
		max_wait_time REAL NOT NULL,
		avg_queue_length REAL NOT NULL,
		max_queue_length INTEGER NOT NULL,
		server_utilization REAL NOT NULL,
		new_patient_avg_wait REAL NOT NULL,
		recheck_patient_avg_wait REAL NOT NULL,
		preemption_count INTEGER NOT NULL,
		new_patient_preempted INTEGER NOT NULL,
		recheck_patient_preempted INTEGER NOT NULL,
		seed INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_timestamp ON simulation_results(timestamp);
	CREATE INDEX IF NOT EXISTS idx_preemption_enabled ON simulation_results(preemption_enabled);
	CREATE INDEX IF NOT EXISTS idx_preemption_threshold ON simulation_results(preemption_threshold);
	`

	_, err = DB.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("创建表失败: %w", err)
	}

	return nil
}

// SaveResult saves a simulation result to the database
func SaveResult(r *SimulationResult) (int64, error) {
	if DB == nil {
		return 0, fmt.Errorf("数据库未初始化")
	}

	insertSQL := `
	INSERT INTO simulation_results (
		timestamp, new_patient_rate, recheck_patient_rate, new_patient_service,
		recheck_service, simulation_duration, preemption_enabled, preemption_threshold,
		total_patients, new_patients, recheck_patients, avg_wait_time, max_wait_time,
		avg_queue_length, max_queue_length, server_utilization, new_patient_avg_wait,
		recheck_patient_avg_wait, preemption_count, new_patient_preempted,
		recheck_patient_preempted, seed
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := DB.Exec(insertSQL,
		r.Timestamp, r.NewPatientRate, r.RecheckPatientRate, r.NewPatientService,
		r.RecheckService, r.SimulationDuration, r.PreemptionEnabled, r.PreemptionThreshold,
		r.TotalPatients, r.NewPatients, r.RecheckPatients, r.AvgWaitTime, r.MaxWaitTime,
		r.AvgQueueLength, r.MaxQueueLength, r.ServerUtilization, r.NewPatientAvgWait,
		r.RecheckPatientAvgWait, r.PreemptionCount, r.NewPatientPreempted,
		r.RecheckPatientPreempted, r.Seed)
	if err != nil {
		return 0, fmt.Errorf("插入结果失败: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取插入ID失败: %w", err)
	}

	return id, nil
}

// QueryResults queries simulation results with optional filters
type QueryFilter struct {
	PreemptionEnabled *bool
	PreemptionThresholdMin *float64
	PreemptionThresholdMax *float64
	NewPatientRateMin *float64
	NewPatientRateMax *float64
	Limit int
	Offset int
}

// QueryResults queries simulation results with optional filters
func QueryResults(filter *QueryFilter) ([]SimulationResult, error) {
	if DB == nil {
		return nil, fmt.Errorf("数据库未初始化")
	}

	query := "SELECT * FROM simulation_results WHERE 1=1"
	args := []interface{}{}

	if filter != nil {
		if filter.PreemptionEnabled != nil {
			query += " AND preemption_enabled = ?"
			args = append(args, *filter.PreemptionEnabled)
		}
		if filter.PreemptionThresholdMin != nil {
			query += " AND preemption_threshold >= ?"
			args = append(args, *filter.PreemptionThresholdMin)
		}
		if filter.PreemptionThresholdMax != nil {
			query += " AND preemption_threshold <= ?"
			args = append(args, *filter.PreemptionThresholdMax)
		}
		if filter.NewPatientRateMin != nil {
			query += " AND new_patient_rate >= ?"
			args = append(args, *filter.NewPatientRateMin)
		}
		if filter.NewPatientRateMax != nil {
			query += " AND new_patient_rate <= ?"
			args = append(args, *filter.NewPatientRateMax)
		}
	}

	query += " ORDER BY timestamp DESC"

	if filter != nil && filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询失败: %w", err)
	}
	defer rows.Close()

	var results []SimulationResult
	for rows.Next() {
		var r SimulationResult
		err := rows.Scan(
			&r.ID, &r.Timestamp, &r.NewPatientRate, &r.RecheckPatientRate,
			&r.NewPatientService, &r.RecheckService, &r.SimulationDuration,
			&r.PreemptionEnabled, &r.PreemptionThreshold, &r.TotalPatients,
			&r.NewPatients, &r.RecheckPatients, &r.AvgWaitTime, &r.MaxWaitTime,
			&r.AvgQueueLength, &r.MaxQueueLength, &r.ServerUtilization,
			&r.NewPatientAvgWait, &r.RecheckPatientAvgWait, &r.PreemptionCount,
			&r.NewPatientPreempted, &r.RecheckPatientPreempted, &r.Seed,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描结果失败: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

// GetRecentResults gets the most recent results
func GetRecentResults(limit int) ([]SimulationResult, error) {
	return QueryResults(&QueryFilter{Limit: limit})
}

// Close closes the database connection
func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}