package network

import (
	"context"
	"fmt"
	"sync"
)

// Server represents a non-blocking DICOM SCP server.
// Unlike SCP.ListenAndServe (which blocks), Server can be started
// and stopped programmatically, allowing multiple servers to run
// simultaneously in the same process.
//
// Example — run 3 servers simultaneously:
//
//	echoServer := network.StartServer(ctx, network.SCPConfig{AETitle: "ECHO", Port: 11112}, &network.EchoHandler{})
//	storeServer := network.StartServer(ctx, network.SCPConfig{AETitle: "STORE", Port: 11113}, storeHandler)
//	wlServer := network.StartServer(ctx, network.SCPConfig{AETitle: "WORKLIST", Port: 11114}, worklistHandler)
//
//	// All three are running concurrently
//	fmt.Println(echoServer.Addr())    // "0.0.0.0:11112"
//	fmt.Println(storeServer.Addr())   // "0.0.0.0:11113"
//	fmt.Println(wlServer.Addr())      // "0.0.0.0:11114"
//
//	// Stop one
//	echoServer.Stop()
//
//	// Wait for all to finish
//	storeServer.Wait()
//	wlServer.Wait()
type Server struct {
	scp    *SCP
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Events provides hooks into server lifecycle events.
	Events *EventManager

	// Logger for this server instance.
	Logger *Logger
}

// StartServer creates and starts a non-blocking DICOM SCP server.
// Returns immediately. The server runs in a background goroutine.
func StartServer(ctx context.Context, config SCPConfig, handler Handler) (*Server, error) {
	// Preserve port 0 (random) before applyDefaults overwrites it
	listenPort := config.Port
	config.applyDefaults()

	scp := NewSCP(config)
	scp.SetHandler(handler)

	serverCtx, cancel := context.WithCancel(ctx)

	s := &Server{
		scp:    scp,
		cancel: cancel,
		Events: NewEventManager(),
		Logger: NewLogger(LogLevelInfo, nil),
	}

	// Start listening first (so we can return errors immediately)
	addr := fmt.Sprintf("%s:%d", config.BindAddress, listenPort)
	ln, err := Listen(addr)
	if err != nil {
		cancel()
		return nil, err
	}

	scp.mu.Lock()
	scp.listener = ln
	scp.running = true
	scp.mu.Unlock()

	s.Logger.Info("Server %s started on %s", config.AETitle, ln.Addr().String())
	s.Events.Emit(&Event{Type: EVTConnOpen, Description: "server started", RemoteAddr: ln.Addr().String()})

	// Run accept loop in background
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer ln.Close()
		defer func() {
			scp.mu.Lock()
			scp.running = false
			scp.mu.Unlock()
		}()

		for {
			transport, err := ln.Accept(serverCtx)
			if err != nil {
				select {
				case <-serverCtx.Done():
					return
				default:
					s.Logger.Error("Accept error: %v", err)
					continue
				}
			}

			s.Events.Emit(&Event{
				Type:       EVTConnOpen,
				RemoteAddr: transport.RemoteAddr().String(),
			})

			scp.wg.Add(1)
			go func() {
				defer scp.wg.Done()
				scp.handleConnection(serverCtx, transport)
				s.Events.Emit(&Event{
					Type:       EVTConnClose,
					RemoteAddr: transport.RemoteAddr().String(),
				})
			}()
		}
	}()

	return s, nil
}

// StartServerTLS creates and starts a TLS-encrypted DICOM SCP server.
func StartServerTLS(ctx context.Context, config SCPConfig, handler Handler, tlsCfg *TLSConfig) (*Server, error) {
	listenPort := config.Port
	config.applyDefaults()

	scp := NewSCP(config)
	scp.SetHandler(handler)

	serverCtx, cancel := context.WithCancel(ctx)

	s := &Server{
		scp:    scp,
		cancel: cancel,
		Events: NewEventManager(),
		Logger: NewLogger(LogLevelInfo, nil),
	}

	addr := fmt.Sprintf("%s:%d", config.BindAddress, listenPort)
	ln, err := ListenTLS(addr, tlsCfg)
	if err != nil {
		cancel()
		return nil, err
	}

	scp.mu.Lock()
	scp.listener = ln
	scp.running = true
	scp.mu.Unlock()

	s.Logger.Info("TLS Server %s started on %s", config.AETitle, ln.Addr().String())

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer ln.Close()
		defer func() {
			scp.mu.Lock()
			scp.running = false
			scp.mu.Unlock()
		}()

		for {
			transport, err := ln.Accept(serverCtx)
			if err != nil {
				select {
				case <-serverCtx.Done():
					return
				default:
					s.Logger.Error("TLS Accept error: %v", err)
					continue
				}
			}

			scp.wg.Add(1)
			go func() {
				defer scp.wg.Done()
				scp.handleConnection(serverCtx, transport)
			}()
		}
	}()

	return s, nil
}

// Stop gracefully stops the server.
func (s *Server) Stop() {
	s.cancel()
	s.scp.Close()
	s.scp.wg.Wait()
}

// Wait blocks until the server has stopped.
func (s *Server) Wait() {
	s.wg.Wait()
}

// Addr returns the server's listening address.
func (s *Server) Addr() string {
	return s.scp.Addr()
}

// SetHandler changes the server's DIMSE handler.
func (s *Server) SetHandler(handler Handler) {
	s.scp.SetHandler(handler)
}

// SetSupportedAbstractSyntaxes configures which SOP Classes this server accepts.
func (s *Server) SetSupportedAbstractSyntaxes(syntaxes []string) {
	s.scp.SetSupportedAbstractSyntaxes(syntaxes)
}

// SetSupportedTransferSyntaxes configures which Transfer Syntaxes this server accepts.
func (s *Server) SetSupportedTransferSyntaxes(syntaxes []string) {
	s.scp.SetSupportedTransferSyntaxes(syntaxes)
}

// ServerGroup manages multiple DICOM servers running simultaneously.
//
// Example:
//
//	group := network.NewServerGroup()
//	group.Add(ctx, network.SCPConfig{AETitle: "ECHO", Port: 11112}, &network.EchoHandler{})
//	group.Add(ctx, network.SCPConfig{AETitle: "STORE", Port: 11113}, storeHandler)
//	group.Add(ctx, network.SCPConfig{AETitle: "QR", Port: 11114}, qrHandler)
//	// ... all 3 running ...
//	group.StopAll()
type ServerGroup struct {
	mu      sync.Mutex
	servers []*Server
}

// NewServerGroup creates a new ServerGroup.
func NewServerGroup() *ServerGroup {
	return &ServerGroup{}
}

// Add creates and starts a new server, adding it to the group.
func (g *ServerGroup) Add(ctx context.Context, config SCPConfig, handler Handler) (*Server, error) {
	s, err := StartServer(ctx, config, handler)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.servers = append(g.servers, s)
	g.mu.Unlock()
	return s, nil
}

// AddTLS creates and starts a new TLS server, adding it to the group.
func (g *ServerGroup) AddTLS(ctx context.Context, config SCPConfig, handler Handler, tlsCfg *TLSConfig) (*Server, error) {
	s, err := StartServerTLS(ctx, config, handler, tlsCfg)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.servers = append(g.servers, s)
	g.mu.Unlock()
	return s, nil
}

// StopAll stops all servers in the group.
func (g *ServerGroup) StopAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, s := range g.servers {
		s.Stop()
	}
}

// WaitAll blocks until all servers have stopped.
func (g *ServerGroup) WaitAll() {
	g.mu.Lock()
	servers := make([]*Server, len(g.servers))
	copy(servers, g.servers)
	g.mu.Unlock()

	for _, s := range servers {
		s.Wait()
	}
}

// Servers returns all servers in the group.
func (g *ServerGroup) Servers() []*Server {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := make([]*Server, len(g.servers))
	copy(result, g.servers)
	return result
}

// Count returns the number of servers in the group.
func (g *ServerGroup) Count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.servers)
}
