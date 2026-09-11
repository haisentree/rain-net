package configstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFileStoreRoundTrip(t *testing.T) {
	s := NewFileStore(t.TempDir())
	ctx := context.Background()

	if _, err := s.Get(ctx, "dns/zones"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing = %v, want ErrNotFound", err)
	}
	if err := s.Put(ctx, "dns/zones", []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "dns/zones")
	if err != nil || string(got) != `{"a":1}` {
		t.Fatalf("get = %q, %v", got, err)
	}
}

func TestFileStoreRejectsBadKeys(t *testing.T) {
	s := NewFileStore(t.TempDir())
	for _, key := range []string{"", "/abs", "../escape", "a/../b"} {
		if _, err := s.Get(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("key %q: err = %v, want ErrInvalidKey", key, err)
		}
		if err := s.Put(context.Background(), key, []byte("x")); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("put %q: err = %v, want ErrInvalidKey", key, err)
		}
	}
}

func TestFileStoreWatch(t *testing.T) {
	s := NewFileStore(t.TempDir())
	s.interval = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := s.Watch(ctx, "routes")

	// 初始状态:文件不存在,不推送
	select {
	case ev := <-ch:
		t.Fatalf("unexpected initial event: %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}

	if err := s.Put(ctx, "routes", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Err != nil {
			t.Fatal(ev.Err)
		}
		if string(ev.Value) != "v1" {
			t.Fatalf("watch = %q, want v1", ev.Value)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not fire")
	}
}
