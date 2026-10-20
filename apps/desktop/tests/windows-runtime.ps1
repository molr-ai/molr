# Run from the extracted Windows acceptance package. Uses only its own task/data.
param([string]$PackageRoot = (Join-Path $PSScriptRoot 'molr'))
$ErrorActionPreference = 'Stop'
$PackageRoot = (Resolve-Path $PackageRoot).Path
$env:MOLR_TEST_BINARY = Join-Path $PackageRoot 'bin\molr.exe'
$setup = Join-Path $PackageRoot 'bin\molr-setup.exe'
$owned = Join-Path ([IO.Path]::GetTempPath()) ('molr-acceptance-' + [Guid]::NewGuid().ToString('N'))
$name = 'molr-desktop-' + [Guid]::NewGuid().ToString('N').Substring(0, 16)
$program = Join-Path $owned 'program'
$installed = $false
$fixturePassword = 'windows-fixture-password-2026'
function Setup([string]$Action) {
  $arguments = @('desktop', $Action, '--name', $name, '--program-root', $program, '--json', '--non-interactive', '--no-browser')
  if ($Action -notin @('status', 'plan')) { $arguments += '--yes' }
  if ($Action -eq 'install') { $arguments += @('--resource-preset', 'permissive') }
  if ($Action -eq 'install' -and -not (Test-Path (Join-Path $program 'data/runtime.db'))) {
    $arguments += @('--init-owner', 'owner')
    $lines = $fixturePassword | & $setup @arguments
  } else { $lines = & $setup @arguments }
  if ($LASTEXITCODE -ne 0) { throw "Installer $Action failed ($LASTEXITCODE)." }
  return (($lines | ForEach-Object { $_ | ConvertFrom-Json }) | Where-Object { $_.type -eq 'desktop' } | Select-Object -Last 1)
}
function Check([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
try {
  New-Item -ItemType Directory -Path $owned | Out-Null
  foreach ($test in @('platform', 'config', 'textfiles', 'sqlitestore', 'installationwindows', 'sandbox')) {
    $binary = Join-Path $PSScriptRoot ($test + '.test.exe')
    & $binary '-test.run=TestWindows|TestControlledReady|TestNativeSandbox' '-test.count=1' '-test.timeout=3m'
    if ($LASTEXITCODE -ne 0) { throw "$test native tests failed ($LASTEXITCODE)." }
  }
  $receipt = Setup 'install'
  $installed = $receipt.installed
  Check $installed 'Runtime was not installed.'
  Check ($receipt.health -eq 'verified') 'Initial authenticated health check failed.'
  $record = Get-Content -Raw $receipt.record | ConvertFrom-Json
  $origin = $receipt.url
  $session = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
  $headers = @{ Origin = $origin }
  $csrf = Invoke-RestMethod -Uri "$origin/api/auth/csrf" -WebSession $session -Headers $headers
  $headers['X-CSRF-Token'] = $csrf.csrf_token
  Check (-not (Test-Path (Join-Path $record.data_dir 'initial-password'))) 'Manual installation saved a plaintext password.'
  $password = $fixturePassword
  $body = @{ username = 'owner'; password = $password; device_name = 'Windows acceptance'; client_type = 'cli' } | ConvertTo-Json
  $auth = Invoke-RestMethod -Method Post -Uri "$origin/api/auth/login" -WebSession $session -Headers $headers -ContentType 'application/json' -Body $body
  $password = $null; $body = $null
  $health = Invoke-RestMethod -Uri "$origin/api/sandbox" -WebSession $session -Headers @{ Origin = $origin }
  Check ($health.Enabled -and $health.Storage -and $health.Capabilities.Reason -eq 'available') 'Native Sandbox is unavailable.'
  foreach ($flag in @('Shell', 'FileIsolation', 'NetworkIsolation', 'FullAccess')) { Check $health.Capabilities.$flag "Missing capability: $flag" }
  Check ($health.Capabilities.NetworkBackend -eq 'appcontainer-toggle') 'Incorrect network backend.'
  Check (-not $health.Capabilities.ResourceLimits -and -not $health.Capabilities.ProcessIsolation) 'Unsupported isolation was advertised.'
  Setup 'upgrade' | Out-Null
  Invoke-RestMethod -Uri "$origin/api/auth/me" -WebSession $session -Headers @{ Origin = $origin } | Out-Null
  Setup 'stop' | Out-Null
  Check ((Setup 'status').state -eq 'inactive') 'Runtime did not stop.'
  Setup 'start' | Out-Null
  Invoke-RestMethod -Uri "$origin/api/auth/me" -WebSession $session -Headers @{ Origin = $origin } | Out-Null
  Setup 'uninstall' | Out-Null
  $installed = $false
  Check (Test-Path (Join-Path $record.data_dir 'runtime.db')) 'Uninstall removed retained data.'
  Check (-not (Test-Path $record.paths.unit)) 'Uninstall retained its task definition.'
  $again = Setup 'install'; $installed = $again.installed
  Check (-not $again.loginURL) 'Reinstallation must not reuse bootstrap credentials.'
  Check ($again.health -eq 'verification_pending') 'Reinstallation health was overstated.'
  Write-Host 'Windows native tests and owned Runtime lifecycle passed.'
} finally {
  # Inspect status even if installation failed after registering its task.
  $status = Setup 'status'
  if ($status.installed) { Setup 'uninstall' | Out-Null }
  if (Test-Path $owned) { Remove-Item -Recurse -Force $owned }
  Remove-Item Env:MOLR_TEST_BINARY -ErrorAction SilentlyContinue
}
