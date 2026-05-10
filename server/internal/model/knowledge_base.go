package model

import "time"

// KnowledgeBase represents a knowledge base that stores and indexes documents.
// Each knowledge base has its own vector storage (SQLite) and HNSW index file.
type KnowledgeBase struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name              string    `gorm:"type:varchar(255);not null" json:"name"`
	Description       string    `gorm:"type:text;not null;default:''" json:"description"`
	EmbeddingConfigID int64     `gorm:"not null;index;column:embedding_config_id" json:"embedding_config_id"`
	IndexType         string    `gorm:"type:varchar(20);not null;default:'flat'" json:"index_type"` // flat/switching/hnsw, runtime state determined by vector count
	IndexFilePath     string    `gorm:"type:varchar(500);not null;default:''" json:"index_file_path"`
	DocumentCount     int       `gorm:"not null;default:0" json:"document_count"`
	VectorCount       int       `gorm:"not null;default:0" json:"vector_count"`
	DeletedCount      int       `gorm:"not null;default:0" json:"deleted_count"`
	CreatedAt         time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (KnowledgeBase) TableName() string { return "knowledge_bases" }
