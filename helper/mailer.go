
package helper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"latihan-fiber/app/model"
)

var (
	ErrTemporaryDelivery = errors.New("pengiriman gagal sementara")
	ErrPermanentDelivery = errors.New("pengiriman gagal permanen")
)

type Mailer interface {
	Send(ctx context.Context, msg model.Message) error
}

type LogMailer struct {
	mu        sync.Mutex
	path      string
	delay     time.Duration
	failEvery int64
	attempts  atomic.Int64
}

func NewLogMailer(path string, delay time.Duration, failEvery int) (Mailer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("menyiapkan folder log email: %w", err)
	}

	return &LogMailer{
		path:      path,
		delay:     delay,
		failEvery: int64(failEvery),
	}, nil
}

func (m *LogMailer) Send(ctx context.Context, msg model.Message) error {
	n := m.attempts.Add(1)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(m.delay):
	}

	if strings.HasSuffix(strings.ToLower(msg.To), ".invalid") {
		return fmt.Errorf("%w: alamat %s tidak ditemukan", ErrPermanentDelivery, msg.To)
	}

	if m.failEvery > 0 && n%m.failEvery == 0 {
		return fmt.Errorf("%w: server email sibuk (attempt ke-%d)", ErrTemporaryDelivery, n)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	f, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("membuka log email: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintf(
		f,
		"to=%s | subject=%s | body=%s\n",
		msg.To,
		msg.Subject,
		msg.Body,
	)
	if err != nil {
		return fmt.Errorf("menulis log email: %w", err)
	}

	return nil
}
