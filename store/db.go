package store

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var buckets = []string{
	"pods",
	"services",
	"configmaps",
	"secrets",
	"deployments",
	"replicasets",
}

type DB struct {
	db *bolt.DB
}

func Open(path string) (*DB, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open bolt db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, bName := range buckets {
			_, err := tx.CreateBucketIfNotExists([]byte(bName))
			if err != nil {
				return fmt.Errorf("failed to create bucket %q: %w", bName, err)
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	return &DB{db: db}, nil
}

func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *DB) Put(bucketName, key string, val interface{}) error {
	data, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("bucket %q not found", bucketName)
		}
		return b.Put([]byte(key), data)
	})
}

func (d *DB) Get(bucketName, key string, out interface{}) (bool, error) {
	var data []byte
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("bucket %q not found", bucketName)
		}
		data = b.Get([]byte(key))
		return nil
	})
	if err != nil {
		return false, err
	}
	if len(data) == 0 {
		return false, nil
	}

	if err := json.Unmarshal(data, out); err != nil {
		return false, fmt.Errorf("failed to unmarshal value: %w", err)
	}
	return true, nil
}

func (d *DB) Delete(bucketName, key string) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("bucket %q not found", bucketName)
		}
		return b.Delete([]byte(key))
	})
}

func (d *DB) List(bucketName string, scanFn func(key, val []byte) error) error {
	return d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("bucket %q not found", bucketName)
		}
		return b.ForEach(scanFn)
	})
}
