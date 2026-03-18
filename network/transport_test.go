package network

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTransportWriteRead(t *testing.T) {
	// Create a pipe to simulate a TCP connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	serverTransport := NewTransport(server, DefaultMaxPDUSize)
	clientTransport := NewTransport(client, DefaultMaxPDUSize)

	ctx := context.Background()

	// Send a ReleaseRQ from client, read on server
	done := make(chan error, 1)
	go func() {
		done <- clientTransport.WritePDU(ctx, &ReleaseRQ{})
	}()

	pdu, err := serverTransport.ReadPDU(ctx)
	if err != nil {
		t.Fatalf("ReadPDU failed: %v", err)
	}

	if _, ok := pdu.(*ReleaseRQ); !ok {
		t.Fatalf("expected *ReleaseRQ, got %T", pdu)
	}

	if err := <-done; err != nil {
		t.Fatalf("WritePDU failed: %v", err)
	}
}

func TestTransportClose(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()

	transport := NewTransport(client, DefaultMaxPDUSize)

	if transport.IsClosed() {
		t.Error("transport should not be closed initially")
	}

	transport.Close()

	if !transport.IsClosed() {
		t.Error("transport should be closed after Close()")
	}

	// Double close should not panic
	transport.Close()
}

func TestTransportMaxPDUSize(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	transport := NewTransport(client, 8192)

	if transport.MaxPDUSize() != 8192 {
		t.Errorf("expected MaxPDUSize 8192, got %d", transport.MaxPDUSize())
	}

	transport.SetMaxPDUSize(32768)
	if transport.MaxPDUSize() != 32768 {
		t.Errorf("expected MaxPDUSize 32768, got %d", transport.MaxPDUSize())
	}
}

func TestTransportDefaultMaxPDUSize(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	transport := NewTransport(client, 0)

	if transport.MaxPDUSize() != DefaultMaxPDUSize {
		t.Errorf("expected default MaxPDUSize %d, got %d", DefaultMaxPDUSize, transport.MaxPDUSize())
	}
}

func TestListener(t *testing.T) {
	ln, err := Listen("127.0.0.1:0") // Random port
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	if addr == "" {
		t.Fatal("listener address should not be empty")
	}

	// Connect a client
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan *Transport, 1)
	go func() {
		transport, err := ln.Accept(ctx)
		if err != nil {
			t.Errorf("Accept failed: %v", err)
			done <- nil
			return
		}
		done <- transport
	}()

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	serverTransport := <-done
	if serverTransport == nil {
		t.Fatal("failed to accept connection")
	}
	defer serverTransport.Close()
}

func TestDial(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Accept in background
	go func() {
		transport, err := ln.Accept(ctx)
		if err != nil {
			return
		}
		transport.Close()
	}()

	transport, err := Dial(ctx, addr, time.Second)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer transport.Close()

	if transport.IsClosed() {
		t.Error("transport should not be closed after Dial")
	}
}

func TestDialFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := Dial(ctx, "127.0.0.1:1", 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected error when connecting to closed port")
	}
}

func TestTransportWriteAfterClose(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()

	transport := NewTransport(client, DefaultMaxPDUSize)
	transport.Close()

	err := transport.WritePDU(context.Background(), &ReleaseRQ{})
	if err == nil {
		t.Error("expected error when writing to closed transport")
	}
}
