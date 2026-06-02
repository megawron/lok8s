package service

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/types"
)

type PodLister interface {
	ListActivePods() []types.Pod
}

type ServiceProxy struct {
	svc      types.Service
	listener net.Listener
	closed   chan struct{}
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	lister   PodLister
	nextIdx  uint64
	port     int
}

func NewServiceProxy(svc types.Service, lister PodLister) *ServiceProxy {
	return &ServiceProxy{
		svc:    svc,
		lister: lister,
		conns:  make(map[net.Conn]struct{}),
		closed: make(chan struct{}),
	}
}

func (sp *ServiceProxy) Start(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	sp.listener = l
	sp.port = port

	go sp.acceptLoop()
	return nil
}

func (sp *ServiceProxy) acceptLoop() {
	for {
		conn, err := sp.listener.Accept()
		if err != nil {
			select {
			case <-sp.closed:
				return
			default:
				return
			}
		}
		go sp.handleConn(conn)
	}
}

func (sp *ServiceProxy) handleConn(in net.Conn) {
	sp.mu.Lock()
	sp.conns[in] = struct{}{}
	sp.mu.Unlock()

	defer func() {
		in.Close()
		sp.mu.Lock()
		delete(sp.conns, in)
		sp.mu.Unlock()
	}()

	backends := sp.getBackends()
	if len(backends) == 0 {
		log.Printf("[service-proxy] No ready backends for service %s/%s", sp.svc.Metadata.Namespace, sp.svc.Metadata.Name)
		return
	}

	// Round-robin load balancing
	idx := atomic.AddUint64(&sp.nextIdx, 1) - 1
	backendPort := backends[idx%uint64(len(backends))]

	targetAddr := fmt.Sprintf("127.0.0.1:%d", backendPort)
	out, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Printf("[service-proxy] Dial backend %s failed: %v", targetAddr, err)
		return
	}
	defer out.Close()

	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(in, out)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(out, in)
		errCh <- err
	}()

	<-errCh
}

func (sp *ServiceProxy) getBackends() []int {
	pods := sp.lister.ListActivePods()
	var backends []int

	for _, pod := range pods {
		if pod.Metadata.Namespace != sp.svc.Metadata.Namespace {
			continue
		}
		if !matchesSelector(pod.Metadata.Labels, sp.svc.Spec.Selector) {
			continue
		}
		
		ready := false
		if pod.Status.Phase == types.PodRunning {
			if len(pod.Status.ContainerStatuses) == 0 {
				ready = true
			} else {
				for _, cs := range pod.Status.ContainerStatuses {
					if cs.Ready {
						ready = true
						break
					}
				}
			}
		}

		if ready && pod.Status.HostPort > 0 {
			backends = append(backends, pod.Status.HostPort)
		}
	}

	return backends
}

func matchesSelector(labels, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if val, ok := labels[k]; !ok || val != v {
			return false
		}
	}
	return true
}

func (sp *ServiceProxy) Close() {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	select {
	case <-sp.closed:
		return
	default:
		close(sp.closed)
	}

	if sp.listener != nil {
		sp.listener.Close()
	}

	for c := range sp.conns {
		c.Close()
	}
}

type ProxyManager struct {
	lister   PodLister
	portPool *network.PortPool
	mu       sync.Mutex
	proxies  map[string]*ServiceProxy
}

func NewProxyManager(lister PodLister, pool *network.PortPool) *ProxyManager {
	return &ProxyManager{
		lister:   lister,
		portPool: pool,
		proxies:  make(map[string]*ServiceProxy),
	}
}

func (pm *ProxyManager) StartProxy(svc *types.Service) (int, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	key := svc.Metadata.Namespace + "/" + svc.Metadata.Name
	if existing, exists := pm.proxies[key]; exists {
		return existing.port, nil
	}

	if len(svc.Spec.Ports) == 0 {
		return 0, fmt.Errorf("service %s has no ports defined", key)
	}

	requestedPort := svc.Spec.Ports[0].Port
	sp := NewServiceProxy(*svc, pm.lister)

	if requestedPort > 0 {
		if err := sp.Start(requestedPort); err == nil {
			pm.proxies[key] = sp
			return requestedPort, nil
		}
	}

	// Fallback/Dynamic allocation from pool
	dynPort, err := pm.portPool.Allocate(key)
	if err != nil {
		return 0, fmt.Errorf("failed to bind to requested port %d and failed to allocate dynamic port: %v", requestedPort, err)
	}

	if err := sp.Start(dynPort); err != nil {
		pm.portPool.Release(key)
		return 0, fmt.Errorf("failed to bind to dynamic port %d: %w", dynPort, err)
	}

	pm.proxies[key] = sp
	return dynPort, nil
}

func (pm *ProxyManager) StopProxy(namespace, name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	key := namespace + "/" + name
	if sp, exists := pm.proxies[key]; exists {
		sp.Close()
		delete(pm.proxies, key)
		pm.portPool.Release(key)
	}
}

func (pm *ProxyManager) Shutdown() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for key, sp := range pm.proxies {
		sp.Close()
		pm.portPool.Release(key)
	}
	pm.proxies = make(map[string]*ServiceProxy)
}

