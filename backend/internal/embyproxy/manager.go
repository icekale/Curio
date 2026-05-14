package embyproxy

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"curio/internal/p115"
	"curio/internal/repository"
)

type PortManager struct {
	store *repository.Store
	play  *p115.Service

	mu     sync.Mutex
	port   int
	server *http.Server
}

func StartPortManager(ctx context.Context, store *repository.Store, play *p115.Service) *PortManager {
	manager := &PortManager{store: store, play: play}
	go manager.run(ctx)
	return manager
}

func (m *PortManager) run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	m.sync(ctx)
	for {
		select {
		case <-ctx.Done():
			m.stop()
			return
		case <-ticker.C:
			m.sync(ctx)
		}
	}
}

func (m *PortManager) sync(ctx context.Context) {
	settings, err := m.store.P115Settings(ctx)
	if err != nil {
		return
	}
	port := settings.EmbyProxyPort
	if port <= 0 {
		port = 8097
	}
	m.mu.Lock()
	current := m.port
	m.mu.Unlock()
	if current == port {
		return
	}
	m.stop()
	if err := m.start(port); err != nil {
		log.Printf("emby proxy listen on :%d failed: %v", port, err)
	}
}

func (m *PortManager) start(port int) error {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           New(m.store, m.play),
		ReadHeaderTimeout: 10 * time.Second,
	}
	m.mu.Lock()
	m.port = port
	m.server = server
	m.mu.Unlock()
	log.Printf("emby proxy listening on :%d", port)
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("emby proxy stopped: %v", err)
		}
	}()
	return nil
}

func (m *PortManager) stop() {
	m.mu.Lock()
	server := m.server
	m.server = nil
	m.port = 0
	m.mu.Unlock()
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
