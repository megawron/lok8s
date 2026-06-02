package network

import (
	"fmt"
	"sync"
)

type PortPool struct {
	mu        sync.Mutex
	minPort   int
	maxPort   int
	allocated map[string]int
	usedPorts map[int]string
}

func NewPortPool(minPort, maxPort int) *PortPool {
	return &PortPool{
		minPort:   minPort,
		maxPort:   maxPort,
		allocated: make(map[string]int),
		usedPorts: make(map[int]string),
	}
}

func (p *PortPool) Allocate(id string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// If already allocated for this ID, return it
	if port, exists := p.allocated[id]; exists {
		return port, nil
	}

	for port := p.minPort; port <= p.maxPort; port++ {
		if _, used := p.usedPorts[port]; !used {
			p.allocated[id] = port
			p.usedPorts[port] = id
			return port, nil
		}
	}

	return 0, fmt.Errorf("no free ports available in range %d-%d", p.minPort, p.maxPort)
}

func (p *PortPool) Release(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if port, exists := p.allocated[id]; exists {
		delete(p.allocated, id)
		delete(p.usedPorts, port)
	}
}

func (p *PortPool) Lookup(id string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if port, exists := p.allocated[id]; exists {
		return port, nil
	}
	return 0, fmt.Errorf("no port allocated for %q", id)
}

func (p *PortPool) Reserve(id string, port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if owner, used := p.usedPorts[port]; used {
		if owner == id {
			return nil
		}
		return fmt.Errorf("port %d already allocated to %s", port, owner)
	}

	p.allocated[id] = port
	p.usedPorts[port] = id
	return nil
}

