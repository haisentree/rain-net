package star

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWindowAcquireRelease(t *testing.T) {
	w := NewWindow(100)
	if err := w.Acquire(context.Background(), 100); err != nil {
		t.Fatal(err)
	}

	// 超出窗口:阻塞直到归还
	done := make(chan error, 1)
	go func() { done <- w.Acquire(context.Background(), 60) }()
	select {
	case err := <-done:
		t.Fatalf("acquire should block, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	w.Release(100)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("acquire did not unblock after release")
	}
}

func TestWindowClose(t *testing.T) {
	w := NewWindow(10)
	w.Close()
	if err := w.Acquire(context.Background(), 5); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("acquire after close = %v, want ErrWindowClosed", err)
	}
}

func TestWindowAcquireLargerThanLimit(t *testing.T) {
	w := NewWindow(100)
	// 250 = 100+100+50 分三次拿全
	go func() {
		time.Sleep(30 * time.Millisecond)
		w.Release(250)
	}()
	if err := w.Acquire(context.Background(), 250); err != nil {
		t.Fatal(err)
	}
}
