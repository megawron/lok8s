package service

import (
	"encoding/json"
	"sync"

	"github.com/megawron/lok8s/store"
	"github.com/megawron/lok8s/types"
)

type Store struct {
	services sync.Map
	db       *store.DB
}

func NewStore(db *store.DB) *Store {
	s := &Store{db: db}
	if db != nil {
		_ = db.List("services", func(key, val []byte) error {
			var svc types.Service
			if err := json.Unmarshal(val, &svc); err == nil {
				s.services.Store(string(key), svc)
			}
			return nil
		})
	}
	return s
}

func (s *Store) Store(svc types.Service) {
	key := svc.Metadata.Namespace + "/" + svc.Metadata.Name
	s.services.Store(key, svc)
	if s.db != nil {
		_ = s.db.Put("services", key, svc)
	}
}

func (s *Store) Load(namespace, name string) (types.Service, bool) {
	key := namespace + "/" + name
	val, ok := s.services.Load(key)
	if !ok {
		return types.Service{}, false
	}
	return val.(types.Service), true
}

func (s *Store) Delete(namespace, name string) {
	key := namespace + "/" + name
	s.services.Delete(key)
	if s.db != nil {
		_ = s.db.Delete("services", key)
	}
}

func (s *Store) List(namespace string) []types.Service {
	var result []types.Service
	s.services.Range(func(_, value any) bool {
		svc := value.(types.Service)
		if namespace == "" || svc.Metadata.Namespace == namespace {
			result = append(result, svc)
		}
		return true
	})
	return result
}
