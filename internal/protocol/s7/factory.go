package s7

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/robinson/gos7"

	"aquacontrolai/internal/protocol"
)

type Factory struct{}

func (Factory) ProtocolType() string { return "S7" }
func (Factory) ValidateConfig(c map[string]any) error {
	if _, ok := c["rack"]; !ok {
		return errors.New("缺少 rack")
	}
	if _, ok := c["slot"]; !ok {
		return errors.New("缺少 slot")
	}
	return nil
}
func (Factory) ValidateAddress(a string, t protocol.DataType, w bool) error {
	return protocol.ValidateS7Address(a, t, w)
}
func (Factory) ConfigSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"rack", "slot"}, "properties": map[string]any{"rack": map[string]any{"type": "integer", "default": 0}, "slot": map[string]any{"type": "integer", "default": 1}}}
}
func (Factory) NewConnection(ctx context.Context, cfg protocol.DeviceConnectionConfig) (protocol.Connection, error) {
	handler := gos7.NewTCPClientHandler(fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), intConfig(cfg.ProtocolConfig, "rack"), intConfig(cfg.ProtocolConfig, "slot"))
	result := make(chan error, 1)
	go func() { result <- handler.Connect() }()
	select {
	case <-ctx.Done():
		_ = handler.Close()
		return nil, ctx.Err()
	case err := <-result:
		if err != nil {
			return nil, err
		}
	}
	return &connection{handler: handler, client: gos7.NewClient(handler)}, nil
}

type address struct {
	area            string
	db, offset, bit int
}
type connection struct {
	mu      sync.Mutex
	handler *gos7.TCPClientHandler
	client  gos7.Client
}

func intConfig(c map[string]any, k string) int {
	switch v := c[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
func parseAddress(raw string) (address, error) {
	parts := strings.Split(raw, ".")
	if len(parts) == 0 || parts[0] == "" {
		return address{}, errors.New("无效S7地址")
	}
	a := address{bit: -1}
	first := parts[0]
	if strings.HasPrefix(first, "DB") {
		a.area = "DB"
		var e error
		a.db, e = strconv.Atoi(strings.TrimPrefix(first, "DB"))
		if e != nil || len(parts) < 2 {
			return a, errors.New("无效DB地址")
		}
		a.offset, e = strconv.Atoi(parts[1])
		if e != nil {
			return a, e
		}
		if len(parts) == 3 {
			a.bit, e = strconv.Atoi(parts[2])
			if e != nil {
				return a, e
			}
		}
	} else {
		a.area = first[:1]
		offsetText := first[1:]
		if len(offsetText) > 1 && (offsetText[0] == 'W' || offsetText[0] == 'D') {
			offsetText = offsetText[1:]
		}
		var e error
		a.offset, e = strconv.Atoi(offsetText)
		if e != nil {
			return a, e
		}
		if len(parts) == 2 {
			a.bit, e = strconv.Atoi(parts[1])
			if e != nil {
				return a, e
			}
		}
	}
	return a, nil
}
func (c *connection) Read(ctx context.Context, raw string, t protocol.DataType) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	a, e := parseAddress(raw)
	if e != nil {
		return 0, e
	}
	size := 2
	if t == protocol.Bool {
		size = 1
	} else if t == protocol.Real {
		size = 4
	}
	buf := make([]byte, size)
	if e = c.read(a, size, buf); e != nil {
		return 0, e
	}
	switch t {
	case protocol.Bool:
		if buf[0]&(1<<a.bit) != 0 {
			return 1, nil
		}
		return 0, nil
	case protocol.Int:
		return float64(int16(binary.BigEndian.Uint16(buf))), nil
	default:
		value := float64(math.Float32frombits(binary.BigEndian.Uint32(buf)))
		return math.Round(value*1000) / 1000, nil
	}
}
func (c *connection) Write(ctx context.Context, raw string, t protocol.DataType, v float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	a, e := parseAddress(raw)
	if e != nil {
		return e
	}
	if t == protocol.Bool {
		buf := []byte{0}
		if e = c.read(a, 1, buf); e != nil {
			return e
		}
		if v != 0 {
			buf[0] |= 1 << a.bit
		} else {
			buf[0] &= ^(1 << a.bit)
		}
		return c.write(a, 1, buf)
	}
	size := 2
	buf := make([]byte, 4)
	if t == protocol.Int {
		binary.BigEndian.PutUint16(buf, uint16(int16(v)))
	} else {
		size = 4
		binary.BigEndian.PutUint32(buf, math.Float32bits(float32(v)))
	}
	return c.write(a, size, buf[:size])
}
func (c *connection) read(a address, size int, b []byte) error {
	switch a.area {
	case "DB":
		return c.client.AGReadDB(a.db, a.offset, size, b)
	case "M":
		return c.client.AGReadMB(a.offset, size, b)
	case "I":
		return c.client.AGReadEB(a.offset, size, b)
	case "Q":
		return c.client.AGReadAB(a.offset, size, b)
	}
	return errors.New("不支持的S7区域")
}
func (c *connection) write(a address, size int, b []byte) error {
	switch a.area {
	case "DB":
		return c.client.AGWriteDB(a.db, a.offset, size, b)
	case "M":
		return c.client.AGWriteMB(a.offset, size, b)
	case "Q":
		return c.client.AGWriteAB(a.offset, size, b)
	}
	return errors.New("不支持的S7写入区域")
}
func (c *connection) Close() error { c.mu.Lock(); defer c.mu.Unlock(); return c.handler.Close() }
