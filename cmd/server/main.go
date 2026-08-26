package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"oral-history-clearance/internal/journal"
)

const defaultAddress = "127.0.0.1:19081"

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Printf("启动失败: %v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseConfig(args, os.Getenv("PORT"))
	if err != nil {
		return err
	}
	if cfg.Selfcheck {
		return runSelfcheck(cfg.Address)
	}
	repo, err := journal.Open(cfg.JournalPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	handler := buildHandler(repo)
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", cfg.Address, err)
	}
	server := newHTTPServer(handler)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("口述史发布放行工作台监听于 http://%s，日志 %s", cfg.Address, cfg.JournalPath)
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
