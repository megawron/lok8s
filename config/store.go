package config

import (
	"sync"

	"github.com/megawron/lok8s/types"
)

type Store struct {
	configMaps sync.Map
	secrets    sync.Map
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) StoreConfigMap(cm types.ConfigMap) {
	key := cm.Metadata.Namespace + "/" + cm.Metadata.Name
	s.configMaps.Store(key, cm)
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
	sec.StringData = nil // Clear stringData since we projected it into Data

	key := sec.Metadata.Namespace + "/" + sec.Metadata.Name
	s.secrets.Store(key, sec)
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
