# NEUDFS GUI (Go)

This is a small Fyne-based GUI client for the existing NEUDFS gRPC server. It does **not** modify server code; it just calls the RPCs defined in `proto/server.proto`.

## Run

From the `server/` directory:

```bash
cd gui
go run .
```

The GUI auto-detects the server address in this order:
1) `NEUDFS_SERVER_ADDR` (or `GRPC_SERVER_ADDR`)
2) `terraform output -raw server_address` (if a `terraform/` dir is found)
3) fallback: `127.0.0.1:50051`

To override the server target, set `NEUDFS_SERVER_ADDR=<host:port>` when launching.
