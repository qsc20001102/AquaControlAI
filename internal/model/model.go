package model

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

type Device struct {
	ID                uuid.UUID       `json:"id"`
	Name              string          `json:"name"`
	ProtocolType      string          `json:"protocol_type"`
	Enabled           bool            `json:"enabled"`
	Host              string          `json:"host"`
	Port              int             `json:"port"`
	ConnectTimeout    int             `json:"connect_timeout"`
	ReconnectInterval int             `json:"reconnect_interval"`
	ProtocolConfig    json.RawMessage `json:"protocol_config"`
	ConnectionStatus  string          `json:"connection_status"`
	LastOnlineAt      *time.Time      `json:"last_online_at,omitempty"`
	LastOfflineAt     *time.Time      `json:"last_offline_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}
type Point struct {
	ID                                          uuid.UUID `json:"id"`
	Name, GroupName                             string
	DeviceID                                    uuid.UUID
	DeviceName, ProtocolType, Address, DataType string
	Unit                                        *string
	Enabled                                     bool
	LatestValue                                 any
	CollectInterval                             int
	StoreHistory                                bool
	HistoryInterval                             int
	HistoryStartedAt                            *time.Time
	WriteEnabled                                bool
	ReadbackTolerance                           float64
	CreatedAt, UpdatedAt                        time.Time
}
