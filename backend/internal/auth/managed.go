package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
)

// EnsureManagedAdmin reserves an identity that cannot be registered or logged in
// with a password. Call only after validating the framework runtime files.
func EnsureManagedAdmin(db *sql.DB, instanceID string) (User, error) {
	if !regexp.MustCompile(`^[a-zA-Z0-9._-]+$`).MatchString(instanceID) {
		return User{}, errors.New("invalid managed instance ID")
	}
	username := "framework-managed:" + instanceID
	const disabledPassword = "!framework-managed:no-password-login"
	tx, err := db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO users(username,password_hash,role,status) VALUES(?,?,'admin','active') ON CONFLICT(username) DO NOTHING`, username, disabledPassword); err != nil {
		return User{}, err
	}
	var user User
	var password string
	err = tx.QueryRow(`SELECT id,username,role,status,created_at,password_hash FROM users WHERE username=?`, username).Scan(&user.ID, &user.Username, &user.Role, &user.Status, &user.CreatedAt, &password)
	if err != nil {
		return User{}, err
	}
	if user.Role != "admin" || user.Status != "active" || password != disabledPassword {
		return User{}, errors.New("managed administrator identity collision or disabled account")
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO wallets(user_id,coins) VALUES(?,0)`, user.ID); err != nil {
		return User{}, err
	}
	return user, tx.Commit()
}

// WithManagedAdmin must only be called AFTER authenticating the local gateway.
func WithManagedAdmin(r *http.Request, user User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userKey, user))
}
