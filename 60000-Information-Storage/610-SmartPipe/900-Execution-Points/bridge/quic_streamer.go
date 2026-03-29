package bridge

import (
	"context"
	"crypto/tls"
	"time"
	"github.com/quic-go/quic-go"
)

type Streamer struct {
	Session quic.Connection
}

// Dial initiates a Sovereign QUIC connection for repository harvesting.
func Dial(ctx context.Context, addr string, tlsConf *tls.Config) (*Streamer, error) {
	conf := &quic.Config{
		MaxIdleTimeout:             30 * time.Second,
		InitialStreamReceiveWindow:  1 << 24, // 16 MB
		MaxStreamReceiveWindow:      1 << 26, // 64 MB
		EnableDatagrams:            true,
	}
	conn, err := quic.DialAddr(ctx, addr, tlsConf, conf)
	if err != nil { return nil, err }
	return &Streamer{Session: conn}, nil
}
