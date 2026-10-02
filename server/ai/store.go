package ai

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type Store struct {
	db *gorm.DB
}

func OpenStore(projectDir string) (*Store, error) {
	if projectDir == "" {
		return nil, fmt.Errorf("project_dir has not been resolved")
	}
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(projectDir, "chat.sqlite"))
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}

	dsn := (&url.URL{Scheme: "file", Path: path}).String() + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	// Disable SQL logging so prompts are not accidentally written to logs.
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	sqlDB, err := db.DB()
	if err != nil {
		if closer, ok := db.ConnPool.(interface{ Close() error }); ok {
			closer.Close()
		}
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&ChatRequest{}); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Save(ctx context.Context, chat *ChatRequest) error {
	if chat == nil {
		return fmt.Errorf("chat request is nil")
	}
	return s.db.WithContext(ctx).Save(chat).Error
}

// ErrChatExists reports that Create was given an ID that is already stored.
var ErrChatExists = errors.New("chat request already exists")

// Create inserts a new chat and never overwrites an existing one, so a client
// cannot replace another conversation by reusing its request ID.
func (s *Store) Create(ctx context.Context, chat *ChatRequest) error {
	if chat == nil {
		return fmt.Errorf("chat request is nil")
	}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(chat)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrChatExists
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (*ChatRequest, error) {
	var chat ChatRequest
	if err := s.db.WithContext(ctx).First(&chat, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &chat, nil
}

func (s *Store) List(ctx context.Context) ([]ChatRequest, error) {
	chats := make([]ChatRequest, 0)
	err := s.db.WithContext(ctx).Order("created_at ASC, id ASC").Find(&chats).Error
	return chats, err
}

func (s *Store) Close() error {
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
