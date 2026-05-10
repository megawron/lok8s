package api

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/discovery"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/manifest"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/service"
	"github.com/megawron/lok8s/types"
)

type Server struct {
	httpServer      *http.Server
	lifecycle       *engine.LifecycleManager
	services        *service.Store
	proxyManager    *service.ProxyManager
	configStore     *config.Store
	controllerStore *controller.Store
	pods            sync.Map
}

func NewServer(addr string, lifecycle *engine.LifecycleManager, portPool *network.PortPool, configStore *config.Store, controllerStore *controller.Store) *Server {
	s := &Server{
		lifecycle:       lifecycle,
		services:        service.NewStore(),
		proxyManager:    service.NewProxyManager(lifecycle, portPool),
		configStore:     configStore,
		controllerStore: controllerStore,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/namespaces/{ns}/pods", s.handleCreatePod)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/pods/{name}/log", s.handleGetPodLogs)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/pods/{name}", s.handleGetPod)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/pods", s.handleListPods)
	mux.HandleFunc("DELETE /api/v1/namespaces/{ns}/pods/{name}", s.handleDeletePod)

	mux.HandleFunc("POST /api/v1/namespaces/{ns}/services", s.handleCreateService)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/services", s.handleListServices)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/services/{name}", s.handleGetService)
	mux.HandleFunc("DELETE /api/v1/namespaces/{ns}/services/{name}", s.handleDeleteService)

	mux.HandleFunc("POST /api/v1/namespaces/{ns}/configmaps", s.handleCreateConfigMap)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/configmaps", s.handleListConfigMaps)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/configmaps/{name}", s.handleGetConfigMap)
	mux.HandleFunc("DELETE /api/v1/namespaces/{ns}/configmaps/{name}", s.handleDeleteConfigMap)

	mux.HandleFunc("POST /api/v1/namespaces/{ns}/secrets", s.handleCreateSecret)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/secrets", s.handleListSecrets)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/secrets/{name}", s.handleGetSecret)
	mux.HandleFunc("DELETE /api/v1/namespaces/{ns}/secrets/{name}", s.handleDeleteSecret)

	// Deployments & ReplicaSets routes
	mux.HandleFunc("POST /apis/apps/v1/namespaces/{ns}/deployments", s.handleCreateDeployment)
	mux.HandleFunc("GET /apis/apps/v1/namespaces/{ns}/deployments", s.handleListDeployments)
	mux.HandleFunc("GET /apis/apps/v1/namespaces/{ns}/deployments/{name}", s.handleGetDeployment)
	mux.HandleFunc("PUT /apis/apps/v1/namespaces/{ns}/deployments/{name}", s.handleUpdateDeployment)
	mux.HandleFunc("DELETE /apis/apps/v1/namespaces/{ns}/deployments/{name}", s.handleDeleteDeployment)

	mux.HandleFunc("POST /apis/apps/v1/namespaces/{ns}/replicasets", s.handleCreateReplicaSet)
	mux.HandleFunc("GET /apis/apps/v1/namespaces/{ns}/replicasets", s.handleListReplicaSets)
	mux.HandleFunc("GET /apis/apps/v1/namespaces/{ns}/replicasets/{name}", s.handleGetReplicaSet)
	mux.HandleFunc("PUT /apis/apps/v1/namespaces/{ns}/replicasets/{name}", s.handleUpdateReplicaSet)
	mux.HandleFunc("DELETE /apis/apps/v1/namespaces/{ns}/replicasets/{name}", s.handleDeleteReplicaSet)

	// K8s compatibility discovery routes
	mux.HandleFunc("GET /api", discovery.HandleAPIRoot)
	mux.HandleFunc("GET /api/v1", discovery.HandleAPIV1)
	mux.HandleFunc("GET /apis", discovery.HandleAPIs)
	mux.HandleFunc("GET /apis/apps/v1", discovery.HandleAPIsAppsV1)
	mux.HandleFunc("GET /version", discovery.HandleVersion)
	mux.HandleFunc("GET /.well-known/openid-configuration", discovery.HandleOpenIDConfig)

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

// PodManager implementation methods
func (s *Server) StorePod(pod types.Pod) {
	s.storePod(pod)
}

func (s *Server) DeletePod(namespace, name string) error {
	_ = s.lifecycle.Terminate(namespace, name)
	s.deletePod(namespace, name)
	return nil
}

func (s *Server) AllPods(namespace string) []types.Pod {
	return s.allPods(namespace)
}

func (s *Server) ListPods(namespace string) []types.Pod {
	pods := s.allPods(namespace)
	for i := range pods {
		if status, managed := s.lifecycle.Status(pods[i].Metadata.Namespace, pods[i].Metadata.Name); managed {
			pods[i].Status = status
		}
	}
	return pods
}

func (s *Server) LaunchPod(pod *types.Pod) error {
	s.storePod(*pod)

	engineType, target, err := manifest.ExtractEngineConfig(pod)
	if err != nil {
		pod.Status.Phase = types.PodFailed
		pod.Status.Message = err.Error()
		s.storePod(*pod)
		return err
	}

	env := manifest.CollectEnvVars(pod.Spec.Containers)
	serviceEnvs := service.GenerateServiceEnv(pod.Metadata.Namespace, s.services, s.proxyManager)
	env = append(env, serviceEnvs...)

	pod.Status = types.PodStatus{
		Phase:     types.PodPending,
		StartTime: time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.lifecycle.Launch(pod, engineType, target, env); err != nil {
		pod.Status.Phase = types.PodFailed
		pod.Status.Message = err.Error()
		s.storePod(*pod)
		return err
	}

	pod.Status.Phase = types.PodRunning
	s.storePod(*pod)
	return nil
}
