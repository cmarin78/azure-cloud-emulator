package storage

import (
	"errors"
	"path/filepath"
	"testing"
)

type sampleItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestStorageCRUD(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	bucket := "mybucket"

	// EnsureBucket
	if err := db.EnsureBucket(bucket); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}

	// Get non-existent
	var item sampleItem
	found, err := db.Get(bucket, "key1", &item)
	if err != nil {
		t.Fatalf("Get non-existent error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for non-existent key")
	}

	// Put
	input := sampleItem{ID: "1", Name: "Azure Emulator"}
	if err := db.Put(bucket, "key1", input); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get existing
	var out sampleItem
	found, err = db.Get(bucket, "key1", &out)
	if err != nil {
		t.Fatalf("Get existing error: %v", err)
	}
	if !found || out.ID != "1" || out.Name != "Azure Emulator" {
		t.Fatalf("Get unexpected result: found=%v, out=%+v", found, out)
	}

	// Delete
	if err := db.Delete(bucket, "key1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	found, _ = db.Get(bucket, "key1", &out)
	if found {
		t.Fatal("expected found=false after Delete")
	}

	// Delete non-existent (should not fail)
	if err := db.Delete(bucket, "key-does-not-exist"); err != nil {
		t.Fatalf("Delete non-existent should succeed: %v", err)
	}
}

func TestStorageList(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	bucket := "listbucket"
	_ = db.Put(bucket, "sub1/rgA/res1", sampleItem{ID: "1"})
	_ = db.Put(bucket, "sub1/rgA/res2", sampleItem{ID: "2"})
	_ = db.Put(bucket, "sub1/rgB/res3", sampleItem{ID: "3"})
	_ = db.Put(bucket, "sub2/rgA/res4", sampleItem{ID: "4"})

	// List prefix "sub1/rgA/"
	var keys []string
	err = db.List(bucket, "sub1/rgA/", func(k string, raw []byte) error {
		keys = append(keys, k)
		return nil
	})
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(keys) != 2 || keys[0] != "sub1/rgA/res1" || keys[1] != "sub1/rgA/res2" {
		t.Fatalf("unexpected keys for prefix sub1/rgA/: %+v", keys)
	}

	// List all (prefix "")
	keys = nil
	err = db.List(bucket, "", func(k string, raw []byte) error {
		keys = append(keys, k)
		return nil
	})
	if err != nil {
		t.Fatalf("List all error: %v", err)
	}
	if len(keys) != 4 {
		t.Fatalf("expected 4 keys, got %d", len(keys))
	}

	// List error propagation
	expectedErr := errors.New("stop iteration")
	err = db.List(bucket, "", func(k string, raw []byte) error {
		return expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}

	// List on non-existent bucket
	err = db.List("non-existent-bucket", "", func(k string, raw []byte) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error on non-existent bucket, got %v", err)
	}
}
