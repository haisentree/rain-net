// Package configstore 提供运行时动态配置的持久化与多节点同步(design.md §七 控制面):
//   - FileStore:目录即键空间,本地持久化(默认实现);
//   - EtcdStore(远期):多节点 watch 同步,接口形状已预留。
//
// 约定:框架只管分发,不理解内容——值由各 handler 自治解释
// (DNS zones、路由规则等,见 design.md §七 原则 2)。
package configstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"
)

// Event 是 Watch 推送的变更事件;Err 非 nil 表示 watch 异常终止。
type Event struct {
	Key   string
	Value []byte
	Err   error
}

// Store 是动态配置的持久化后端接口。
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, val []byte) error
	// Watch 持续观察一个键,变更即推送;ctx 取消后通道关闭。
	Watch(ctx context.Context, key string) <-chan Event
}

var (
	ErrNotFound   = errors.New("configstore: key not found")
	ErrInvalidKey = errors.New("configstore: invalid key")
)

// ValidKey 键约束:相对路径形状,禁止 .. 与绝对路径(防目录逃逸)。
func ValidKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	if path.Clean(key) != key {
		return fmt.Errorf("%w: %q not normalized", ErrInvalidKey, key)
	}
	return nil
}

// ---- FileStore ----

// FileStore 目录实现的 Store:一个键 = 根目录下的一个文件。
// Watch 采用轮询(modtime 比对),间隔默认 500ms——本地持久化够用,
// 多节点同步走 EtcdStore(远期)。
type FileStore struct {
	root     string
	interval time.Duration
}

func NewFileStore(root string) *FileStore {
	return &FileStore{root: root, interval: 500 * time.Millisecond}
}

func (s *FileStore) path(key string) (string, error) {
	if err := ValidKey(key); err != nil {
		return "", err
	}
	return s.root + "/" + key, nil
}

func (s *FileStore) Get(_ context.Context, key string) ([]byte, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	data, err := readFile(p)
	if err != nil {
		if errors.Is(err, errFileNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

func (s *FileStore) Put(_ context.Context, key string, val []byte) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	return writeFile(p, val)
}

func (s *FileStore) Watch(ctx context.Context, key string) <-chan Event {
	ch := make(chan Event, 8)
	go func() {
		defer close(ch)
		// 基线:Watch 启动时的 modtime(不存在则为零值),之后只报变更
		var last time.Time
		if st, err := os.Stat(s.root + "/" + key); err == nil {
			last = st.ModTime()
		}
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p, err := s.path(key)
				if err != nil {
					ch <- Event{Key: key, Err: err}
					return
				}
				mod, err := modTime(p)
				if err != nil {
					if !last.IsZero() { // 文件被删除
						last = time.Time{}
						ch <- Event{Key: key, Err: ErrNotFound}
						return
					}
					continue
				}
				if mod.After(last) {
					last = mod
					val, err := s.Get(ctx, key)
					if err != nil {
						ch <- Event{Key: key, Err: err}
						return
					}
					ch <- Event{Key: key, Value: val}
				}
			}
		}
	}()
	return ch
}
