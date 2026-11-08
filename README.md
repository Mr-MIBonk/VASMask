# VASMask

A D-PDU API (ISO 22900-2) shim that lets a VAS5054A interface show up as a
VAS6154 to an MVCI server. It forwards every call to the real D-PDU API and
rewrites only identity data: module names, `PDUGetVersion` data, module type
and resource ids.

- **32-bit**: wraps the Softing D-PDU API. At install time the mask MDF/CDF are
  generated from the Softing MDF/CDF already installed on the machine, with the
  VAS5054 module type and the OBD cable renamed.
- **64-bit**: wraps the VW D-PDU API, which reaches the VAS5054A as a J2534
  module. That module is reported as module type VAS6154, using the resource
  map built from the installed VW MDF.

`VAS6154Mask.exe` / `VAS6154Mask64.exe` is a small GUI that tests the shim
against the connected interface, installs it into the D-PDU API root file and
restores the original state.

## Requirements

- Windows; a VAS5054A with its Softing D-PDU API (32-bit) or the VW D-PDU API (64-bit) installed
- Build: [Zig](https://ziglang.org/) (as C compiler) and [Go](https://go.dev/) 1.27+

## Build

```powershell
.\build.ps1
```

or from a POSIX shell (e.g. Git Bash):

```sh
./build.sh
```

Releases are built by GitHub Actions (`.github/workflows/build.yml`): every
push is built and tested, and a push to `main` publishes release `v<VERSION>`
with a zip of `dist\` when that release does not exist yet. To release a new
version, bump `VERSION`.

The output goes to `dist\` (32-bit) and `dist\x64\` (64-bit). Tests:
`cd tester; go test .` (some tests read the real D-PDU API files and are
skipped when those are not installed).

## Configuration

`vas6154shim.ini` next to the DLL; see `shim/vas6154shim.ini` and
`shim/vas6154shim64.ini` for the documented keys. With `Log=1` the shim writes
a call log next to the DLL.

## Legal notice

This repository contains only original code. It ships no files from Softing,
Volkswagen or other vendors: the D-PDU API header is a minimal declaration
written from the ISO 22900-2 interface, and the MDF/CDF are derived locally
from the user's own installation.

You need your own licensed copies of the D-PDU APIs and diagnostic software
you use this with. You are responsible for making sure your use complies with
their license terms. VAS, VAS5054A, VAS6154 and ODIS are names and trademarks
of their respective owners, used here only to describe compatibility. This
project is not affiliated with or endorsed by them.

Released under the [MIT License](LICENSE).
