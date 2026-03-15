package api

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/types"
)

type Server struct {
	httpServer *http.Server
	lifecycle  *engine.LifecycleManager
	pods       sync.Map
}

func NewServer(addr string, lifecycle *engine.LifecycleManager) *Server {
	s := &Server{
		lifecycle: lifecycle,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/namespaces/{ns}/pods", s.handleCreatePod)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/pods/{name}/log", s.handleGetPodLogs)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/pods/{name}", s.handleGetPod)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/pods", s.handleListPods)
	mux.HandleFunc("DELETE /api/v1/namespaces/{ns}/pods/{name}", s.handleDeletePod)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

func (s *Server) storePod(pod types.Pod) {
	key := pod.Metadata.Namespace + "/" + pod.Metadata.Name
	s.pods.Store(key, pod)
}

func (s *Server) loadPod(namespace, name string) (types.Pod, bool) {
	val, ok := s.pods.Load(namespace + "/" + name)
	if !ok {
		return types.Pod{}, false
	}
	return val.(types.Pod), true
}

func (s *Server) deletePod(namespace, name string) {
	s.pods.Delete(namespace + "/" + name)
}

func (s *Server) allPods(namespace string) []types.Pod {
	var result []types.Pod
	s.pods.Range(func(_, value any) bool {
		pod := value.(types.Pod)
		if namespace == "" || pod.Metadata.Namespace == namespace {
			result = append(result, pod)
		}
		return true
	})
	return result
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}

	log.Printf("lok8s apiserver listening on %s", ln.Addr().String())
	return s.httpServer.Serve(ln)
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("lok8s apiserver shutting down")
	return s.httpServer.Shutdown(ctx)
}
