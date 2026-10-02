package systemdunit

import _ "embed"

// UserService is the packaged systemd user unit installed by the CLI.
//
//go:embed linkmyphone.service
var UserService []byte
