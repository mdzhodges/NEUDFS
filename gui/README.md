# NEUDFS GUI (Go)

This is a small Fyne-based GUI client for the existing NEUDFS gRPC server. It does **not** modify server code; it just calls the RPCs defined in `proto/server.proto`.

## Run

From the `server/` directory:

```bash
cd gui
go run .
```

## Server address auto-detect

The GUI defaults the server address like this:

1) `NEUDFS_SERVER_ADDR` env var (if set)
2) `GRPC_SERVER_ADDR` env var (if set)
3) If a local Terraform state exists, it tries `terraform output -raw server_address`
4) Fallback: `127.0.0.1:50051`

If `go run .` exits immediately, check the terminal output—on Linux it must be run in a desktop session (needs `DISPLAY` or `WAYLAND_DISPLAY`), and some sandboxed environments require `XDG_CONFIG_HOME` to point to a writable directory (the GUI will fall back to `/tmp/neudfs-gui-config` when needed).

## Linux build deps (Fyne)

On Linux, Fyne’s desktop driver uses GLFW and needs common X11/OpenGL development headers installed. On Ubuntu/Debian this is typically:

```bash
sudo apt-get install -y xorg-dev libgl1-mesa-dev libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev
```

## Notes

- The server expects an `email` value in gRPC metadata for **every** RPC.
- Server address defaults to `127.0.0.1:50051`.
- Upload/Download use streaming RPCs; the GUI prompts for local files via dialogs.

## Deployed upload troubleshooting

If upload works locally but fails on the deployed ECS service with `code = Internal desc = failed to upload file`, the server failed its S3 `PutObject` call. Common causes:

- ECS task env is missing `S3_BUCKET` (server defaults to `neudfs-storage-dev`, which won’t exist in AWS).
- The ECS task role lacks S3 permissions for the created bucket.

This repo’s Terraform passes `S3_BUCKET` into the ECS task and adds an S3 bucket policy that allows the ECS LabRole to `ListBucket`, `GetObject`, `PutObject`, and `DeleteObject` on the created bucket. After applying Terraform, rebuild/push the server image and redeploy.
