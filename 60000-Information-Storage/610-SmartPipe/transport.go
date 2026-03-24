package main

import (
	"context"
	"fmt"
	"crypto/tls"
	"github.com/quic-go/quic-go"
)

// Transport sets up a QUIC pipe for repo data
type Transport struct {
	Addr string
}

// Connect dial a QUIC connection to the target
func (t *Transport) Connect(ctx context.Context) (quic.Connection, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true, // Phase 1: Skip for MVP; Phase 2: SPIFFE/SVID
		NextProtos:         []string{"git-sovereign"},
	}
	return quic.DialAddr(ctx, t.Addr, tlsConf, nil)
}

// PipeBundle streams a bundle across a QUIC stream
func (t *Transport) PipeBundle(ctx context.Context, conn quic.Connection, s *SmartPipe) error {
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("failed to open QUIC stream: %w", err)
	}
	defer stream.Close()

	// Direct memory-pipe from Git Bundle to QUIC stream
	fmt.Println("🚀 Commencing Zero-Disk Harvest via Smart Pipe...")
	return s.GenerateBundle(stream)
}

// StartServer listens for incoming rehydrations
func (t *Transport) StartServer(ctx context.Context) error {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"git-sovereign"},
	}
	listener, err := quic.ListenAddr(t.Addr, tlsConf, nil)
	if err != nil {
		return err
	}
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return err
		}
		go t.handleIncoming(conn)
	}
}

func (t *Transport) handleIncoming(conn quic.Connection) {
	// Rehydration logic in Phase 2
}
