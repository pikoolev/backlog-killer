$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repo = Split-Path -Parent $PSScriptRoot
Push-Location $repo
try {
  python scripts/deploy_native.py
  if ($LASTEXITCODE -ne 0) { throw 'Native deployment failed' }
} finally {
  Pop-Location
}
