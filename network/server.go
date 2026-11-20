package network

import (
	"context"
	"fmt"
	"net"
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
	if l := loggerFor(config.Logger); l != nil {
		s.Logger = l
	}
	if config.Events != nil {
		s.Events = config.Events
	}

	// Start listening first (so we can return errors immediately)
	addr := fmt.Sprintf("%s:%d", config.BindAddress, listenPort)
	ln, err := listen(addr, config.TLS)
	if err != nil {
		cancel()
		return nil, err
	}
	s.run(serverCtx, ln)
	return s, nil
}

// StartServerOn is StartServer for a listener the caller made: the server
// accepts on l and does not bind an address of its own, so the caller decides
// how the socket is made, which of IPv4 and IPv6 a wildcard covers for one,
// and there is no moment between the bind and the serving in which the port
// can be taken or a connection find nothing accepting. config.BindAddress and
// config.Port are not used.
//
// When config.TLS is set, l is wrapped for TLS as StartServer does for the
// listener it makes, so l is the plain TCP listener. Stop closes l, and so
// does Shutdown when its drain ends. When StartServerOn returns an error, for
// a TLS configuration that cannot be used, l has not been touched and is
// still the caller's to close.
func StartServerOn(ctx context.Context, config SCPConfig, handler Handler, l net.Listener) (*Server, error) {
	config.applyDefaults()

	scp := NewSCP(config)
	scp.SetHandler(handler)

	ln, err := wrapListener(l, config.TLS)
	if err != nil {
		return nil, err
	}
	serverCtx, cancel := context.WithCancel(ctx)

	s := &Server{
		scp:    scp,
		cancel: cancel,
		Events: NewEventManager(),
		Logger: NewLogger(LogLevelInfo, nil),
	}
	if lg := loggerFor(config.Logger); lg != nil {
		s.Logger = lg
	}
	if config.Events != nil {
		s.Events = config.Events
	}
	s.run(serverCtx, ln)
	return s, nil
}

// run starts the accept loop of a server on a listener that is open, and
// reports the server started.
func (s *Server) run(serverCtx context.Context, ln *Listener) {
	scp, config := s.scp, s.scp.config
	scp.mu.Lock()
	scp.listener = ln
	scp.running = true
	scp.mu.Unlock()

	s.Logger.Info("Server %s started on %s", config.AETitle, ln.Addr().String())
	s.Events.emit(&Event{Type: EVTConnOpen, Description: "server started", RemoteAddr: ln.Addr().String()}, scp.ownLog)

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

		_ = scp.serve(serverCtx, ln, s.Logger, s.Events)
	}()
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
	if l := loggerFor(config.Logger); l != nil {
		s.Logger = l
	}
	if config.Events != nil {
		s.Events = config.Events
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

		_ = scp.serve(serverCtx, ln, s.Logger, s.Events)
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

// SetSupportedAbstractSyntaxes configures which SOP Classes this server
// accepts, for associations negotiated from then on. The accept loop is
// already running when StartServer returns, so SCPConfig.AbstractSyntaxes is
// the way to set them before the first.
func (s *Server) SetSupportedAbstractSyntaxes(syntaxes []string) {
	s.scp.SetSupportedAbstractSyntaxes(syntaxes)
}

// ReportStorageCommitment sends a deferred commitment result on a new
// association it opens to the requestor.
//
// The counterpart to returning a result marked Deferred from a
// StorageCommitmentProvider: the request is acknowledged, the archive verifies
// durability on its own schedule, and this delivers the answer once there is
// one. See SCP.ReportStorageCommitment.
func (s *Server) ReportStorageCommitment(ctx context.Context, requestorAE string,
	result *StorageCommitmentResult) error {
	return s.scp.ReportStorageCommitment(ctx, requestorAE, result)
}

// ReportUPSEvent delivers a UPS N-EVENT-REPORT to a subscriber, over an
// association this server opens to it. See SCP.ReportUPSEvent.
func (s *Server) ReportUPSEvent(ctx context.Context, receivingAE string, event *UPSEvent) error {
	return s.scp.ReportUPSEvent(ctx, receivingAE, event)
}

// SetSupportedTransferSyntaxes configures which Transfer Syntaxes this server
// accepts, for associations negotiated from then on; SCPConfig.TransferSyntaxes
// sets them before the first.
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
