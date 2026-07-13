package modbus

import (
	"encoding/binary"
	"math"
	"testing"

	"aquacontrolai/internal/protocol"
)

func TestValidateAddress(t *testing.T) {
	f := Factory{}
	tests := []struct {
		name      string
		address   string
		dataType  protocol.DataType
		writable  bool
		wantError bool
	}{
		{name: "holding real read", address: "40001", dataType: protocol.Real, wantError: false},
		{name: "holding real write", address: "40005", dataType: protocol.Real, writable: true, wantError: false},
		{name: "input real read", address: "30001", dataType: protocol.Real, wantError: false},
		{name: "input real write denied", address: "30001", dataType: protocol.Real, writable: true, wantError: true},
		{name: "coil bool write", address: "00001", dataType: protocol.Bool, writable: true, wantError: false},
		{name: "coil real denied", address: "00001", dataType: protocol.Real, wantError: true},
		{name: "discrete bool read", address: "10001", dataType: protocol.Bool, wantError: false},
		{name: "gap denied", address: "20001", dataType: protocol.Int, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.ValidateAddress(tt.address, tt.dataType, tt.writable)
			if (err != nil) != tt.wantError {
				t.Fatalf("ValidateAddress() error=%v wantError=%v", err, tt.wantError)
			}
		})
	}
}

func TestFloat32OrderCDAB(t *testing.T) {
	const value = 12.5
	var ieee [4]byte
	binary.BigEndian.PutUint32(ieee[:], math.Float32bits(value))
	raw := fromIEEEBytes(math.Float32bits(value), "CDAB")
	wantRaw := []byte{ieee[2], ieee[3], ieee[0], ieee[1]}
	for i := range raw {
		if raw[i] != wantRaw[i] {
			t.Fatalf("fromIEEEBytes CDAB byte %d=%02x want %02x", i, raw[i], wantRaw[i])
		}
	}
	decoded := math.Float32frombits(binary.BigEndian.Uint32(toIEEEBytes(raw, "CDAB")))
	if math.Abs(float64(decoded)-value) > 0.0001 {
		t.Fatalf("decoded=%v want %v", decoded, value)
	}
}
