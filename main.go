package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Setup the Docker client and clear any existing PR containers.
	docker, err := NewDocker()
	if err != nil {
		panic(fmt.Errorf("new docker: %w", err))
	}
	defer docker.Close()

	if err = docker.ClearContainers(); err != nil {
		panic(fmt.Errorf("clear containers: %w", err))
	}

	// Create the router and start it in a goroutine.
	router := NewRouter(docker, os.Getenv("API_KEY"))
	go func() {
		if err := router.Run(":8080"); err != nil {
			panic(fmt.Errorf("run router: %w", err))
		}
	}()

	// Gracefully handle shutdown signals.
	c := make(chan os.Signal, 2)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	// Set up the listener and start listening for connections.
	listener := NewListener(docker)
	go func() {
		<-c
		listener.Close()
	}()
	if err := listener.Listen(":19132"); err != nil {
		panic(fmt.Errorf("listen: %w", err))
	}
}
