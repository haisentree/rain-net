package decorator

import (
	"context"
	"github.com/haisentree/rain-net/pkg/rainnet"
)

type rateLimitedConn struct {
	rainnet.Conn
	bkt *bucket
}

func (r *rateLimitedConn) Read(p []byte) (int, error) {
	if err := r.bkt.take(context.Background(), float64(len(p))); err != nil {
		return 0, err
	}
	return r.Conn.Read(p)
}

func (r *rateLimitedConn) Write(p []byte) (int, error) {
	if err := r.bkt.take(context.Background(), float64(len(p))); err != nil {
		return 0, err
	}
	return r.Conn.Write(p)
}

// NewRateLimit 按字节速率限速(bytesPerSec,读写双向各自扣桶)。
func NewRateLimit(c rainnet.Conn, bytesPerSec float64) rainnet.Conn {
	if bytesPerSec <= 0 {
		return c
	}
	return &rateLimitedConn{Conn: c, bkt: newBucket(bytesPerSec)}
}
