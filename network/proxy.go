package network

import (
	"io"
	"log"
	"net"
	"sync"
)

type Proxy struct {
	listenAddr string
	targetAddr string
	listener   net.Listener
	mu         sync.Mutex
	conns      map[net.Conn]struct{}
	closed     chan struct{}
}

func NewProxy(listenAddr, targetAddr string) *Proxy {
	return &Proxy{
		listenAddr: listenAddr,
		targetAddr: targetAddr,
		conns:      make(map[net.Conn]struct{}),
		closed:     make(chan struct{}),
	}
}

func (p *Proxy) Start() error {
	l, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return err
	}
	p.listener = l

	go p.acceptLoop()
	return nil
}

func (p *Proxy) acceptLoop() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.closed:
				return
			default:
				return
			}
		}

		go p.handleConn(conn)
	}
}

func (p *Proxy) handleConn(in net.Conn) {
	p.mu.Lock()
	p.conns[in] = struct{}{}
	p.mu.Unlock()

	defer func() {
		in.Close()
		p.mu.Lock()
		delete(p.conns, in)
		p.mu.Unlock()
	}()

	out, err := net.Dial("tcp", p.targetAddr)
	if err != nil {
		log.Printf("[proxy] Dial target %s failed: %v", p.targetAddr, err)
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

func (p *Proxy) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case <-p.closed:
		return
	default:
		close(p.closed)
	}

	if p.listener != nil {
		p.listener.Close()
	}

	for c := range p.conns {
		c.Close()
	}
}
