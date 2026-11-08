# Builds the shim DLL (zig cc) and the Go tester into dist\ (32-bit, Softing
# D-PDU API) and dist\x64\ (64-bit, VW D-PDU API). Same steps as build.sh.
# Requires zig and go on PATH (or $env:ZIG pointing to zig.exe).
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$zig = if ($env:ZIG) { $env:ZIG } else { 'zig' }

function Run([string]$exe, [string[]]$argv) {
	& $exe @argv
	if ($LASTEXITCODE -ne 0) { throw "$exe failed with exit code $LASTEXITCODE" }
}

New-Item -ItemType Directory -Force dist, dist\x64, build | Out-Null

Run $zig @('cc', '-target', 'x86-windows-gnu', '-O2', '-shared', '-Wall',
	'-o', 'dist\PDUAPI_VAS6154_Shim.dll', 'shim\shim.c', 'shim\shim.def')
# x64 has no stdcall name decoration: export the plain names.
(Get-Content shim\shim.def) -replace ' = .*', '' | Set-Content -Encoding ascii build\shim64.def
Run $zig @('cc', '-target', 'x86_64-windows-gnu', '-O2', '-shared', '-Wall',
	'-o', 'dist\x64\PDUAPI_VAS6154_Shim.dll', 'shim\shim.c', 'build\shim64.def', 'shim\shim64.rc')
Remove-Item dist\*.lib, dist\*.pdb, dist\x64\*.lib, dist\x64\*.pdb -ErrorAction SilentlyContinue

if (-not (Test-Path dist\vas6154shim.ini)) { Copy-Item shim\vas6154shim.ini dist\ }
if (-not (Test-Path dist\x64\vas6154shim.ini)) { Copy-Item shim\vas6154shim64.ini dist\x64\vas6154shim.ini }

$env:CGO_ENABLED = '0'
Push-Location tester
try {
	$env:GOARCH = '386'
	Run go @('build', '-ldflags', '-H windowsgui', '-o', '..\dist\VAS6154Mask.exe', '.')
	$env:GOARCH = 'amd64'
	Run go @('build', '-ldflags', '-H windowsgui', '-o', '..\dist\x64\VAS6154Mask64.exe', '.')
} finally {
	Pop-Location
	Remove-Item Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
}
Get-ChildItem dist, dist\x64 -File | Format-Table Name, Length -AutoSize
