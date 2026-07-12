package protocol

import "testing"

func TestValidateS7Address(t *testing.T) {
	tests := []struct {
		address             string
		dataType            DataType
		writable, wantError bool
	}{{"DB2.1186.0", Real, false, false}, {"DB2.1186.1", Real, false, true}, {"MD540", Real, true, false}, {"MW540", Int, true, false}, {"M4.0", Bool, true, false}, {"I1.0", Bool, true, true}}
	for _, tt := range tests {
		t.Run(tt.address+string(tt.dataType), func(t *testing.T) {
			err := ValidateS7Address(tt.address, tt.dataType, tt.writable)
			if (err != nil) != tt.wantError {
				t.Fatalf("ValidateS7Address() error=%v wantError=%v", err, tt.wantError)
			}
		})
	}
}
