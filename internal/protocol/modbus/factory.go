package modbus

import (
	"aquacontrolai/internal/protocol"
	"context"
	"errors"
	"strconv"
)

type Factory struct{}

func (Factory) ProtocolType() string { return "MODBUS_TCP" }
func (Factory) ValidateConfig(c map[string]any) error {
	u, ok := c["unit_id"].(float64)
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
	n, e := strconv.Atoi(a)
	if e != nil || n < 1 || n > 49999 {
		return errors.New("Modbus 地址无效")
	}
	prefix := n / 10000
	if prefix <= 1 && t != protocol.Bool {
		return errors.New("线圈/离散输入仅支持BOOL")
	}
	if prefix >= 3 && t == protocol.Bool {
		return errors.New("寄存器仅支持INT/REAL")
	}
	if w && (prefix == 1 || prefix == 3) {
		return errors.New("该区域只读")
	}
	return nil
}
func (Factory) ConfigSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"unit_id", "float32_order"}}
}
func (Factory) NewConnection(context.Context, protocol.DeviceConnectionConfig) (protocol.Connection, error) {
	return nil, errors.New("Modbus 连接由运行时适配器创建")
}
