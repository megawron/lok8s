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
	"github.com/megawron/lok8s/store"
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

	// Open local database
	db, err := store.Open("lok8s.db")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer func() {
		log.Println("closing database...")
		if err := db.Close(); err != nil {
			log.Printf("database close error: %v", err)
		}
	}()

	registry := engine.NewRegistry()
	registry.Register("native", engine.NewNativeEngine())
	registry.Register("wasm", engine.NewWasmEngine())

	portPool := network.NewPortPool(30000, 32767)
	configStore := config.NewStore(db)
	controllerStore := controller.NewStore(db)

	lifecycle := engine.NewLifecycleManager(registry, portPool, configStore)
	srv := api.NewServer(*addr, lifecycle, portPool, configStore, controllerStore, db)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize and start controllers
	rsController := controller.NewReplicaSetController(controllerStore, srv)
	depController := controller.NewDeploymentController(controllerStore)

	log.Println("Starting ReplicaSet and Deployment controllers")
	rsController.Start(ctx)
	depController.Start(ctx)

	// Recover state of services and pods
	srv.RecoverState()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	case <-ctx.Done():
		log.Println("received shutdown signal, shutting down services and pods...")
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Printf("shutdown error: %v", err)
		}
		lifecycle.Shutdown()
	}
}

