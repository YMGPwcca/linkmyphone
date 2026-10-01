package controlplane

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
)

const (
	ProtocolVersion         = 1
	MaxRequestBytes         = 1 << 20
	maxUnixSocketPathLength = 100
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
	Version   int                   `json:"version"`
	Operation Operation             `json:"operation"`
	ID        string                `json:"id,omitempty"`
	Record    *kernel.FeatureRecord `json:"record,omitempty"`
	Enabled   *bool                 `json:"enabled,omitempty"`
	Config    *json.RawMessage      `json:"config,omitempty"`
}

type Response struct {
	Version   int                    `json:"version"`
	OK        bool                   `json:"ok"`
	Error     string                 `json:"error,omitempty"`
	Record    *kernel.FeatureRecord  `json:"record,omitempty"`
	Records   []kernel.FeatureRecord `json:"records,omitempty"`
	Snapshot  *kernel.Snapshot       `json:"snapshot,omitempty"`
	Snapshots []kernel.Snapshot      `json:"snapshots,omitempty"`
}

type Handler interface {
	HandleControl(Request) Response
}

type HandlerFunc func(Request) Response

func (f HandlerFunc) HandleControl(request Request) Response {
	return f(request)
}

func SocketPathForStore(storePath string) string {
	clean := filepath.Clean(storePath)
	if absolute, err := filepath.Abs(clean); err == nil {
		clean = absolute
	}

	sum := sha256.Sum256([]byte(clean))
	socketName := hex.EncodeToString(sum[:12]) + ".sock"

	root := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if root != "" {
		candidate := filepath.Join(root, "phonelink-linux", socketName)
		if len(candidate) < maxUnixSocketPathLength {
			return candidate
		}
	}

	// Linux sockaddr_un paths are small (typically 108 bytes including the
	// terminator). Use a deliberately short, user-scoped fallback instead of
	// inheriting an arbitrarily deep XDG/TMP path.
	return filepath.Join(
		"/tmp",
		"phonelink-linux-"+strconv.Itoa(os.Getuid()),
		socketName,
	)
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
