package service

import (
	"sync"

	"github.com/megawron/lok8s/types"
)

type Store struct {
	services sync.Map
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) Store(svc types.Service) {
	key := svc.Metadata.Namespace + "/" + svc.Metadata.Name
	s.services.Store(key, svc)
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
