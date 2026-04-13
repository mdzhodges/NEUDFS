# NEUDFS GUI (Go)

This is a small Fyne-based GUI client for the existing NEUDFS gRPC server. It does **not** modify server code; it just calls the RPCs defined in `proto/server.proto`.

## Run

From the `server/` directory:

```bash
cd gui
go run .
```
