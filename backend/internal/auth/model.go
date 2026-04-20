package auth

import "time"

type User struct {
	ID          string    `json:"id"`
	IdentityKey string    `json:"identity_key"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
