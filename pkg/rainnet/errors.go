package rainnet

import (
	"errors"
	"fmt"
)

// ErrKind 错误三分类(docs/design.md §九.4),为 metrics 与日志提供稳定维度:
//   - KindNetwork:底层网络错误(连接重置、超时、不可达)——通常值得重试/告警;
//   - KindProtocol:协议语义错误(握手失败、帧格式非法)——对端实现问题;
//   - KindPolicy:策略拒绝(认证失败、限流、admission、白名单)——正常业务拒绝,不应计为故障。
type ErrKind int

const (
	KindUnknown ErrKind = iota
	KindNetwork
	KindProtocol
	KindPolicy
)

func (k ErrKind) String() string {
	switch k {
	case KindNetwork:
		return "network"
	case KindProtocol:
		return "protocol"
	case KindPolicy:
		return "policy"
	default:
		return "unknown"
	}
}

// Error 携带分类的包装错误。
type Error struct {
	Kind ErrKind
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("%s error: %v", e.Kind, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// NetworkError/ProtocolError/PolicyError 构造分类错误。
func NetworkError(err error) error { return &Error{Kind: KindNetwork, Err: err} }
func ProtocolError(err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: KindProtocol, Err: err}
}
func PolicyError(err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: KindPolicy, Err: err}
}

// KindOf 提取错误的分类;未经分类包装的错误返回 KindUnknown(网络层裸错误按需自行归类)。
func KindOf(err error) ErrKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindUnknown
}
