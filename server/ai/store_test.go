package ai

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestStorePersistsChat(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "project ? #", ".violet")
	store, err := OpenStore(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	chat := ChatRequest{Messages: []ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello!"},
	}}
	ctx := context.Background()
	if err := store.Save(ctx, &chat); err != nil {
		t.Fatal(err)
	}
	if chat.Id == uuid.Nil || chat.CreatedAt.IsZero() || chat.UpdatedAt.IsZero() {
		t.Fatalf("missing identity or timestamps: %+v", chat)
	}
	if chat.Recipient != DefaultModel {
		t.Fatalf("unexpected recipient: %s", chat.Recipient)
	}
	chat.Messages = append(chat.Messages, ChatMessage{Role: "assistant", Content: "Hi!"})
	if err := store.Save(ctx, &chat); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(ctx, chat.Id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Id != chat.Id || loaded.Recipient != chat.Recipient || !reflect.DeepEqual(loaded.Messages, chat.Messages) {
		t.Fatalf("chat did not round-trip: %+v", loaded)
	}
	if !loaded.CreatedAt.Equal(chat.CreatedAt) || !loaded.UpdatedAt.Equal(chat.UpdatedAt) {
		t.Fatal("timestamps did not round-trip")
	}
	chats, err := store.List(ctx)
	if err != nil || len(chats) != 1 {
		t.Fatalf("updates must not duplicate chats: %d, %v", len(chats), err)
	}
}

func TestStoreUsesWAL(t *testing.T) {
	projectDir := t.TempDir()
	for range 2 {
		store, err := OpenStore(projectDir)
		if err != nil {
			t.Fatal(err)
		}
		var mode string
		if err := store.db.Raw("PRAGMA journal_mode").Scan(&mode).Error; err != nil {
			store.Close()
			t.Fatal(err)
		}
		if mode != "wal" {
			store.Close()
			t.Fatalf("journal_mode = %q, want wal", mode)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreErrorsAndRecipient(t *testing.T) {
	if _, err := OpenStore(""); err == nil {
		t.Fatal("expected unresolved project_dir error")
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.Save(ctx, nil); err == nil {
		t.Fatal("expected nil chat error")
	}
	if _, err := store.Get(ctx, uuid.New()); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record-not-found, got %v", err)
	}
	chat := ChatRequest{Id: uuid.New(), Recipient: "custom-model"}
	if err := store.Save(ctx, &chat); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(ctx, chat.Id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Recipiant() != "custom-model" {
		t.Fatal("custom recipient was not persisted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Save(cancelled, &ChatRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
