package model

import "time"

// User represents the human user of the private-buddy system.
// Initially single-user; the table structure supports future multi-user expansion.
// Name is the immutable identity anchor used by agents when forming
// EntityProfile narratives about the user.
type User struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string    `gorm:"type:varchar(255);not null;uniqueIndex" json:"name"` // Immutable identity key
	Bio       string    `gorm:"type:text;not null;default:''" json:"bio"`            // Optional one-line self-description
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (User) TableName() string { return "users" }
