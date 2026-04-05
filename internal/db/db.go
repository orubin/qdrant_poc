package db

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"qdrant-poc/pkg/models"
)

type DB struct {
	conn *sql.DB
}

func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite: %w", err)
	}

	if err := createSchema(conn); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &DB{conn: conn}, nil
}

func createSchema(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	_, err := db.Exec(query)
	return err
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) SaveMessage(role, content string) error {
	_, err := db.conn.Exec("INSERT INTO messages (role, content) VALUES (?, ?)", role, content)
	return err
}

func (db *DB) GetMessages() ([]models.ChatMessage, error) {
	rows, err := db.conn.Query("SELECT role, content FROM messages ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.ChatMessage
	for rows.Next() {
		var msg models.ChatMessage
		if err := rows.Scan(&msg.Role, &msg.Content); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}
