package storage

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

type MySQLConfig struct {
	Host     string
	Database string
	Username string
	Password string
	Table    string
}

type MySQLStorage struct {
	db    *sql.DB
	table string
}

type LagRecord struct {
	StatsDate       string
	BatchNo         string
	Topic           string
	ConsumerGroup   string
	TotalLag        int64
	MaxLag          int64
	CurrentLag      int64
	AvgLag          int64
	PartitionCounts int
	IsAlert         bool
	Threshold       int64
	RecordTime      string
}

func NewMySQLStorage(config MySQLConfig) *MySQLStorage {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8&parseTime=True",
		config.Username,
		config.Password,
		config.Host,
		config.Database,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(fmt.Sprintf("Failed to connect to MySQL: %v", err))
	}

	// Test connection
	if err = db.Ping(); err != nil {
		panic(fmt.Sprintf("Failed to ping MySQL: %v", err))
	}

	return &MySQLStorage{
		db:    db,
		table: config.Table,
	}
}

func (s *MySQLStorage) InsertLagRecord(record LagRecord) error {
	query := fmt.Sprintf(`
		INSERT INTO %s
			(stats_date, batch_no, topic, consumer_group, total_lag, max_lag, current_lag, avg_lag, partition_counts, is_alert, threshold, record_time)
		VALUES
			(%q, %q, %q, %q, %d, %d, %d, %d, %d, %t, %d, %q)
	`, s.table,
		record.StatsDate,
		record.BatchNo,
		record.Topic,
		record.ConsumerGroup,
		record.TotalLag,
		record.MaxLag,
		record.CurrentLag,
		record.AvgLag,
		record.PartitionCounts,
		record.IsAlert,
		record.Threshold,
		record.RecordTime)

	// fmt.Println(query)

	_, err := s.db.Exec(query)

	return err
}
