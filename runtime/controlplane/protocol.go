package controlplane

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
)

const (
	ProtocolVersion = 1
	MaxRequestBytes = 1 << 20
)

type Operation string

const (
	OperationList   Operation = "list"
	OperationGet    Operation = "get"
	OperationCreate Operation = "create"
	OperationUpdate Operation = "update"
	OperationDelete Operation = "delete"
)

var (
	ErrUnavailable    = errors.New("controlplane: runtime unavailable")
	ErrAlreadyRunning = errors.New("controlplane: runtime already running")
)

type Request struct {
	Version   int                  `json:"version"`
	Operation Operation            `json:"operation"`
	ID        string               `json:"id,omitempty"`
	Record    *kernel.FeatureRecord `json:"record,omitempty"`
	Enabled   *bool                `json:"enabled,omitempty"`
	Config    *json.RawMessage     `json:"config,omitempty"`
}

type Response struct {
	Version   int                   `json:"version"`
	OK        bool                  `json:"ok"`
	Error     string                `json:"error,omitempty"`
	Record    *kernel.FeatureRecord `json:"record,omitempty"`
	Records   []kernel.FeatureRecord `json:"records,omitempty"`
	Snapshot  *kernel.Snapshot      `json:"snapshot,omitempty"`
	Snapshots []kernel.Snapshot     `json:"snapshots,omitempty"`
}

type Handler interface {
	HandleControl(Request) Response
}

type HandlerFunc func(Request) Response

func (f HandlerFunc) HandleControl(request Request) Response {
	return f(request)
}

func SocketPathForStore(storePath string) string {
	return filepath.Join(filepath.Dir(storePath), "runtime.sock")
}

func Success() Response {
	return Response{Version: ProtocolVersion, OK: true}
}

func Failure(err error) Response {
	response := Response{Version: ProtocolVersion, OK: false}
	if err != nil {
		response.Error = err.Error()
	}
	return response
}
