$ErrorActionPreference = 'Stop'

Write-Host 'Running race-enabled Go tests...'
go test -race ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host 'Running go vet...'
go vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host 'Validating Docker Compose configuration...'
docker compose -f deploy/docker-compose.yml config --quiet
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host 'All checks passed.'
