package modbus

import (
	"aquacontrolai/internal/protocol"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"sync"
	"time"
)

type Factory struct{}

func (Factory) ProtocolType() string { return "MODBUS_TCP" }
func (Factory) ValidateConfig(c map[string]any) error {
	u, ok := intConfig(c, "unit_id")
	if !ok || u < 1 || u > 247 {
		return errors.New("unit_id 必须在1~247")
	}
	o, ok := c["float32_order"].(string)
	if !ok || !map[string]bool{"ABCD": true, "BADC": true, "CDAB": true, "DCBA": true}[o] {
		return errors.New("float32_order 无效")
	}
	return nil
}
func (Factory) ValidateAddress(a string, t protocol.DataType, w bool) error {
	_, e := parseAddress(a, t, w)
	return e
}
func (Factory) ConfigSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"unit_id", "float32_order"}}
}
func (Factory) NewConnection(ctx context.Context, cfg protocol.DeviceConnectionConfig) (protocol.Connection, error) {
	unitID, ok := intConfig(cfg.ProtocolConfig, "unit_id")
	if !ok || unitID < 1 || unitID > 247 {
		return nil, errors.New("unit_id 必须在1~247")
	}
	order, ok := cfg.ProtocolConfig["float32_order"].(string)
	if !ok || !map[string]bool{"ABCD": true, "BADC": true, "CDAB": true, "DCBA": true}[order] {
		return nil, errors.New("float32_order 无效")
	}
	dialer := &net.Dialer{Timeout: time.Duration(cfg.ConnectTimeoutSeconds) * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.ConnectTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &connection{conn: conn, unitID: byte(unitID), float32Order: order, timeout: timeout}, nil
}

type address struct {
	zeroBased uint16
	readFC    byte
}

type connection struct {
	mu           sync.Mutex
	conn         net.Conn
	unitID       byte
	float32Order string
	timeout      time.Duration
	txID         uint16
}

func intConfig(c map[string]any, k string) (int, bool) {
	switch v := c[k].(type) {
	case float64:
		if math.Trunc(v) != v {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	}
	return 0, false
}

func parseAddress(raw string, t protocol.DataType, writable bool) (address, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 49999 {
		return address{}, errors.New("Modbus 地址无效")
	}
	var a address
	switch {
	case n >= 1 && n <= 9999:
		if t != protocol.Bool {
			return address{}, errors.New("线圈仅支持BOOL")
		}
		a = address{zeroBased: uint16(n - 1), readFC: 1}
	case n >= 10001 && n <= 19999:
		if t != protocol.Bool {
			return address{}, errors.New("离散输入仅支持BOOL")
		}
		if writable {
			return address{}, errors.New("离散输入只读")
		}
		a = address{zeroBased: uint16(n - 10001), readFC: 2}
	case n >= 30001 && n <= 39999:
		if t == protocol.Bool {
			return address{}, errors.New("输入寄存器仅支持INT/REAL")
		}
		if writable {
			return address{}, errors.New("输入寄存器只读")
		}
		a = address{zeroBased: uint16(n - 30001), readFC: 4}
	case n >= 40001 && n <= 49999:
		if t == protocol.Bool {
			return address{}, errors.New("保持寄存器仅支持INT/REAL")
		}
		a = address{zeroBased: uint16(n - 40001), readFC: 3}
	default:
		return address{}, errors.New("Modbus 地址范围无效")
	}
	return a, nil
}

func (c *connection) Read(ctx context.Context, raw string, t protocol.DataType) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, err := parseAddress(raw, t, false)
	if err != nil {
		return 0, err
	}
	quantity := uint16(1)
	if t == protocol.Real {
		quantity = 2
	}
	pdu := []byte{a.readFC, byte(a.zeroBased >> 8), byte(a.zeroBased), byte(quantity >> 8), byte(quantity)}
	resp, err := c.request(ctx, pdu)
	if err != nil {
		return 0, err
	}
	if len(resp) < 2 || resp[0] != a.readFC {
		return 0, errors.New("Modbus 读取响应无效")
	}
	byteCount := int(resp[1])
	if len(resp) != 2+byteCount {
		return 0, errors.New("Modbus 读取响应长度无效")
	}
	data := resp[2:]
	switch t {
	case protocol.Bool:
		if byteCount < 1 {
			return 0, errors.New("Modbus BOOL 响应长度无效")
		}
		if data[0]&1 != 0 {
			return 1, nil
		}
		return 0, nil
	case protocol.Int:
		if byteCount != 2 {
			return 0, errors.New("Modbus INT 响应长度无效")
		}
		return float64(int16(binary.BigEndian.Uint16(data))), nil
	case protocol.Real:
		if byteCount != 4 {
			return 0, errors.New("Modbus REAL 响应长度无效")
		}
		value := float64(math.Float32frombits(binary.BigEndian.Uint32(toIEEEBytes(data, c.float32Order))))
		return math.Round(value*1000) / 1000, nil
	}
	return 0, errors.New("不支持的数据类型")
}

func (c *connection) Write(ctx context.Context, raw string, t protocol.DataType, value float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, err := parseAddress(raw, t, true)
	if err != nil {
		return err
	}
	var pdu []byte
	switch t {
	case protocol.Bool:
		out := uint16(0)
		if value != 0 {
			out = 0xFF00
		}
		pdu = []byte{5, byte(a.zeroBased >> 8), byte(a.zeroBased), byte(out >> 8), byte(out)}
	case protocol.Int:
		out := uint16(int16(value))
		pdu = []byte{6, byte(a.zeroBased >> 8), byte(a.zeroBased), byte(out >> 8), byte(out)}
	case protocol.Real:
		rawBytes := fromIEEEBytes(math.Float32bits(float32(value)), c.float32Order)
		pdu = []byte{16, byte(a.zeroBased >> 8), byte(a.zeroBased), 0, 2, 4}
		pdu = append(pdu, rawBytes...)
	default:
		return errors.New("不支持的数据类型")
	}
	resp, err := c.request(ctx, pdu)
	if err != nil {
		return err
	}
	if len(resp) < 1 || resp[0] != pdu[0] {
		return errors.New("Modbus 写入响应无效")
	}
	if pdu[0] == 16 {
		if len(resp) != 5 || resp[1] != pdu[1] || resp[2] != pdu[2] || resp[3] != 0 || resp[4] != 2 {
			return errors.New("Modbus 写多个寄存器响应无效")
		}
		return nil
	}
	if len(resp) != len(pdu) {
		return errors.New("Modbus 写入响应长度无效")
	}
	for i := range pdu {
		if resp[i] != pdu[i] {
			return errors.New("Modbus 写入响应回显不一致")
		}
	}
	return nil
}

func (c *connection) request(ctx context.Context, pdu []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.txID++
	frame := make([]byte, 7+len(pdu))
	binary.BigEndian.PutUint16(frame[0:2], c.txID)
	binary.BigEndian.PutUint16(frame[2:4], 0)
	binary.BigEndian.PutUint16(frame[4:6], uint16(len(pdu)+1))
	frame[6] = c.unitID
	copy(frame[7:], pdu)
	if err := c.setDeadline(ctx); err != nil {
		return nil, err
	}
	if _, err := c.conn.Write(frame); err != nil {
		return nil, err
	}
	header := make([]byte, 7)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint16(header[0:2]) != c.txID || binary.BigEndian.Uint16(header[2:4]) != 0 || header[6] != c.unitID {
		return nil, errors.New("Modbus MBAP 响应无效")
	}
	length := int(binary.BigEndian.Uint16(header[4:6]))
	if length < 2 || length > 253 {
		return nil, errors.New("Modbus MBAP 长度无效")
	}
	resp := make([]byte, length-1)
	if _, err := io.ReadFull(c.conn, resp); err != nil {
		return nil, err
	}
	if len(resp) >= 2 && resp[0] == pdu[0]|0x80 {
		return nil, fmt.Errorf("Modbus 异常响应: function=%d exception=%d", pdu[0], resp[1])
	}
	return resp, nil
}

func (c *connection) setDeadline(ctx context.Context) error {
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return c.conn.SetDeadline(deadline)
}

func toIEEEBytes(raw []byte, order string) []byte {
	switch order {
	case "ABCD":
		return []byte{raw[0], raw[1], raw[2], raw[3]}
	case "BADC":
		return []byte{raw[1], raw[0], raw[3], raw[2]}
	case "CDAB":
		return []byte{raw[2], raw[3], raw[0], raw[1]}
	case "DCBA":
		return []byte{raw[3], raw[2], raw[1], raw[0]}
	default:
		return []byte{raw[0], raw[1], raw[2], raw[3]}
	}
}

func fromIEEEBytes(bits uint32, order string) []byte {
	ieee := make([]byte, 4)
	binary.BigEndian.PutUint32(ieee, bits)
	switch order {
	case "ABCD":
		return []byte{ieee[0], ieee[1], ieee[2], ieee[3]}
	case "BADC":
		return []byte{ieee[1], ieee[0], ieee[3], ieee[2]}
	case "CDAB":
		return []byte{ieee[2], ieee[3], ieee[0], ieee[1]}
	case "DCBA":
		return []byte{ieee[3], ieee[2], ieee[1], ieee[0]}
	default:
		return []byte{ieee[0], ieee[1], ieee[2], ieee[3]}
	}
}

func (c *connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}
