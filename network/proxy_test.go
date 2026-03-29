package network

import (
	"net"
	"testing"
	"time"
)

func TestProxy_TCPForwarding(t *testing.T) {
	// 1. Start target TCP server
	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start target listener: %v", err)
	}
	defer targetListener.Close()

	targetAddr := targetListener.Addr().String()

	go func() {
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				_, _ = c.Write(append([]byte("echo: "), buf[:n]...))
			}(conn)
		}
	}()

	// 2. Start proxy
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	proxyListener.Close() // Close it so proxy can bind to it

	p := NewProxy(proxyAddr, targetAddr)
	if err := p.Start(); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	defer p.Close()

	// 3. Connect to proxy and test forwarding
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("failed to dial proxy: %v", err)
	}
	defer conn.Close()

	testMsg := "hello lok8s"
	_, err = conn.Write([]byte(testMsg))
	if err != nil {
		t.Fatalf("failed to write to proxy: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read from proxy: %v", err)
	}

	expected := "echo: hello lok8s"
	if string(buf[:n]) != expected {
		t.Errorf("expected %q, got %q", expected, string(buf[:n]))
	}

	// 4. Verify Close cleans up connections
	p.Close()
	time.Sleep(50 * time.Millisecond) // Let it settle

	_, err = net.Dial("tcp", proxyAddr)
	if err == nil {
		t.Error("expected dial to closed proxy to fail, but it succeeded")
	}
}
