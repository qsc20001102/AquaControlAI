package protocol

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"regexp"
	"sync"
)

type DataType string

const (
	Bool DataType = "BOOL"
	Int  DataType = "INT"
	Real DataType = "REAL"
)

type DeviceConnectionConfig struct {
	DeviceID              uuid.UUID
	Host                  string
	Port                  int
	ConnectTimeoutSeconds int
	ProtocolConfig        map[string]any
}
type Connection interface {
	Read(context.Context, string, DataType) (float64, error)
	Write(context.Context, string, DataType, float64) error
	Close() error
}
type Factory interface {
	ProtocolType() string
	ValidateConfig(map[string]any) error
	ValidateAddress(string, DataType, bool) error
	ConfigSchema() map[string]any
	NewConnection(context.Context, DeviceConnectionConfig) (Connection, error)
}
type Registry struct {
	mu    sync.RWMutex
	items map[string]Factory
}

func NewRegistry() *Registry { return &Registry{items: map[string]Factory{}} }
func (r *Registry) Register(f Factory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[f.ProtocolType()]; ok {
		return fmt.Errorf("协议已注册: %s", f.ProtocolType())
	}
	r.items[f.ProtocolType()] = f
	return nil
}
func (r *Registry) Get(t string) (Factory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.items[t]
	if !ok {
		return nil, errors.New("不支持的协议")
	}
	return f, nil
}
func (r *Registry) Metadata() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []map[string]any{}
	for _, f := range r.items {
		port := 502
		if f.ProtocolType() == "S7" {
			port = 102
		}
		out = append(out, map[string]any{"protocol_type": f.ProtocolType(), "default_port": port, "config_schema": f.ConfigSchema()})
	}
	return out
}

var s7DB = regexp.MustCompile(`^DB\d+\.\d+(?:\.[0-7])?$`)
var s7Standard = regexp.MustCompile(`^[MIQ]\d+(?:\.[0-7])?$`)
var s7Word = regexp.MustCompile(`^[MQ][WD]\d+$`)

func ValidateS7Address(a string, t DataType, w bool) error {
	if !s7DB.MatchString(a) && !s7Standard.MatchString(a) && !s7Word.MatchString(a) {
		return errors.New("S7 地址格式无效")
	}
	hasBit := regexp.MustCompile(`\.[0-7]$`).MatchString(a)
	numericDBZeroSuffix := t != Bool && regexp.MustCompile(`^DB\d+\.\d+\.0$`).MatchString(a)
	if t == Bool && !hasBit {
		return errors.New("BOOL 地址必须包含 bit")
	}
	if t != Bool && hasBit && !numericDBZeroSuffix {
		return errors.New("INT/REAL 地址不能包含 bit（仅兼容 DB 数值地址末尾 .0）")
	}
	if t == Int && regexp.MustCompile(`^[MQ]D`).MatchString(a) {
		return errors.New("INT 应使用 MW/QW 地址")
	}
	if t == Real && regexp.MustCompile(`^[MQ]W`).MatchString(a) {
		return errors.New("REAL 应使用 MD/QD 地址")
	}
	if w && len(a) > 0 && a[0] == 'I' {
		return errors.New("I 区只读")
	}
	return nil
}
