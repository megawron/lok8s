package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/megawron/lok8s/api"
	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/network"
)

const banner = `
  ██╗      ██████╗ ██╗  ██╗ █████╗ ███████╗
  ██║     ██╔═══██╗██║ ██╔╝██╔══██╗██╔════╝
  ██║     ██║   ██║█████╔╝ ╚█████╔╝███████╗
  ██║     ██║   ██║██╔═██╗ ██╔══██╗╚════██║
  ███████╗╚██████╔╝██║  ██╗╚█████╔╝███████║
  ╚══════╝ ╚═════╝ ╚═╝  ╚═╝ ╚════╝ ╚══════╝
  lightweight pod supervisor — no containers required
`

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	fmt.Fprint(os.Stderr, banner)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	registry := engine.NewRegistry()
	registry.Register("native", engine.NewNativeEngine())
	registry.Register("wasm", engine.NewWasmEngine())

	portPool := network.NewPortPool(30000, 32767)
	configStore := config.NewStore()
	controllerStore := controller.NewStore()

	lifecycle := engine.NewLifecycleManager(registry, portPool, configStore)
	srv := api.NewServer(*addr, lifecycle, portPool, configStore, controllerStore)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize and start controllers
	rsController := controller.NewReplicaSetController(controllerStore, srv)
	depController := controller.NewDeploymentController(controllerStore)

	log.Println("Starting ReplicaSet and Deployment controllers")
	rsController.Start(ctx)
	depController.Start(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	case <-ctx.Done():
		log.Println("received shutdown signal")
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}
}
