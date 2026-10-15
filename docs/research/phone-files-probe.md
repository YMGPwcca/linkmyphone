# Phone Files F1 diagnostic

Run `linkmyphone phone-files-probe --execute` with an upgraded **running** daemon. It uses the daemon's existing PL identity and sends one PLATFORM DeviceResourceManager GET for `/filesmetadata`, with full-sync first-batch protobuf and an explicit deadline. It does not enroll, download file content, request thumbnails, delete/rename, or save metadata.

The probe is not a feature and is never auto-run. Its output includes only response status, count, size, time, and batching flags. Errors and protocol response sizes are bounded. The remote request may still cause phone-side metadata reads and temporary caching. Do not publish captures or device identities.

Install the branch binary with `go build -o /tmp/linkmyphone-f1 ./cmd/linkmyphone`; use your existing systemd service ExecStart and backup the binary before replacing it. Replacing the binary alone does not update a running process: restart the service once after installation. Do not run a second runtime. Then run the one-shot CLI command. Check `linkmyphone feature list` afterward.

Only a matched PLATFORM response with DRM success and `FilesGetResponse.status=1` (SUCCESS) establishes F1. A timeout may reflect an unsupported PL identity or routing and does not establish a permissions failure. The codec requests a batched full-sync response and intentionally follows no continuation token. It cannot guarantee a small response if Android ignores the batched flag; the decoder enforces its local 4 MiB limit.

This is experimental interoperability research, not completed Gallery support.
