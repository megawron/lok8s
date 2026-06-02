package config

import (
	"encoding/json"
	"sync"

	"github.com/megawron/lok8s/store"
	"github.com/megawron/lok8s/types"
)

type Store struct {
	configMaps sync.Map
	secrets    sync.Map
	db         *store.DB
}

func NewStore(db *store.DB) *Store {
	s := &Store{db: db}
	if db != nil {
		_ = db.List("configmaps", func(key, val []byte) error {
			var cm types.ConfigMap
			if err := json.Unmarshal(val, &cm); err == nil {
				s.configMaps.Store(string(key), cm)
			}
			return nil
		})
		_ = db.List("secrets", func(key, val []byte) error {
			var sec types.Secret
			if err := json.Unmarshal(val, &sec); err == nil {
				s.secrets.Store(string(key), sec)
			}
			return nil
		})
	}
	return s
}

func (s *Store) StoreConfigMap(cm types.ConfigMap) {
	key := cm.Metadata.Namespace + "/" + cm.Metadata.Name
	s.configMaps.Store(key, cm)
	if s.db != nil {
		_ = s.db.Put("configmaps", key, cm)
	}
}

func (s *Store) LoadConfigMap(namespace, name string) (types.ConfigMap, bool) {
	key := namespace + "/" + name
	val, ok := s.configMaps.Load(key)
	if !ok {
		return types.ConfigMap{}, false
	}
	return val.(types.ConfigMap), true
}

func (s *Store) DeleteConfigMap(namespace, name string) {
	key := namespace + "/" + name
	s.configMaps.Delete(key)
	if s.db != nil {
		_ = s.db.Delete("configmaps", key)
	}
}

func (s *Store) ListConfigMaps(namespace string) []types.ConfigMap {
	var result []types.ConfigMap
	s.configMaps.Range(func(_, value any) bool {
		cm := value.(types.ConfigMap)
		if namespace == "" || cm.Metadata.Namespace == namespace {
			result = append(result, cm)
		}
		return true
	})
	return result
}

func (s *Store) StoreSecret(sec types.Secret) {
	if sec.Data == nil {
		sec.Data = make(map[string][]byte)
	}
	for k, v := range sec.StringData {
		sec.Data[k] = []byte(v)
	}
	sec.StringData = nil

	key := sec.Metadata.Namespace + "/" + sec.Metadata.Name
	s.secrets.Store(key, sec)
	if s.db != nil {
		_ = s.db.Put("secrets", key, sec)
	}
}

func (s *Store) LoadSecret(namespace, name string) (types.Secret, bool) {
	key := namespace + "/" + name
	val, ok := s.secrets.Load(key)
	if !ok {
		return types.Secret{}, false
	}
	return val.(types.Secret), true
}

func (s *Store) DeleteSecret(namespace, name string) {
	key := namespace + "/" + name
	s.secrets.Delete(key)
	if s.db != nil {
		_ = s.db.Delete("secrets", key)
	}
}

func (s *Store) ListSecrets(namespace string) []types.Secret {
	var result []types.Secret
	s.secrets.Range(func(_, value any) bool {
		sec := value.(types.Secret)
		if namespace == "" || sec.Metadata.Namespace == namespace {
			result = append(result, sec)
		}
		return true
	})
	return result
}
