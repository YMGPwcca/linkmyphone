package systemdunit

import _ "embed"

// UserService is the packaged systemd user unit installed by the CLI.
//
//go:embed phonelink-linux.service
var UserService []byte
