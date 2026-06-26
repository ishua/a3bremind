package store

import (
	"database/sql"
	"fmt"
	"time"
)

// InsertInstanceReply inserts a mapping from a Telegram reply_message_id to an instance_id.
func InsertInstanceReply(db Querier, replyMessageID int, instanceID string) error {
	// Use INSERT OR REPLACE so that repeat notifications update the mapping.
	const query = `INSERT OR REPLACE INTO instance_replies (reply_message_id, instance_id, created_at)
		VALUES (?, ?, ?)`
	now := time.Now()
	_, err := db.Exec(query, replyMessageID, instanceID, now.Unix())
	if err != nil {
		return fmt.Errorf("insert instance reply: %w", err)
	}
	return nil
}

// GetInstanceIDByReply retrieves the instance_id associated with a reply_message_id.
func GetInstanceIDByReply(db Querier, replyMessageID int) (string, error) {
	const query = `SELECT instance_id FROM instance_replies WHERE reply_message_id = ?`
	var instanceID string
	err := db.QueryRow(query, replyMessageID).Scan(&instanceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("instance reply for message %d not found", replyMessageID)
		}
		return "", fmt.Errorf("get instance id by reply: %w", err)
	}
	return instanceID, nil
}
