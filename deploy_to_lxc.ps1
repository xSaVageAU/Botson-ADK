# deploy_to_lxc.ps1
# Automates compiling and deploying the botson-web Linux binary to the remote Proxmox LXC container.

$ErrorActionPreference = "Stop"

# Remote target details
$RemoteHost = "root@192.168.69.40"
$RemotePath = "/root/botson-web-linux-amd64"
$TempPath = "/root/botson-web-linux-amd64-new"
$ServiceName = "botson-web"

Write-Host "=== 1. Compiling botson-web for Linux amd64 ===" -ForegroundColor Cyan
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -o build/botson-web/botson-web-linux-amd64 ./apps/botson-web
Write-Host "Compilation complete: build/botson-web/botson-web-linux-amd64" -ForegroundColor Green

Write-Host "`n=== 2. Uploading binary to remote container ($RemoteHost) ===" -ForegroundColor Cyan
scp build/botson-web/botson-web-linux-amd64 "${RemoteHost}:${TempPath}"
Write-Host "Upload complete." -ForegroundColor Green

Write-Host "`n=== 3. Swapping binary and restarting remote service ===" -ForegroundColor Cyan
ssh $RemoteHost "mv $TempPath $RemotePath && chmod +x $RemotePath && systemctl restart $ServiceName"
Write-Host "Binary swapped and service restarted successfully." -ForegroundColor Green

Write-Host "`n=== 4. Checking service status ===" -ForegroundColor Cyan
ssh $RemoteHost "systemctl status $ServiceName"
