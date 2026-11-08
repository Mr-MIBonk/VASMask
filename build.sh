#!/bin/sh
# Builds the shim DLL (zig cc) and the Go tester into dist/ (32-bit, Softing
# D-PDU API) and dist/x64/ (64-bit, VW D-PDU API). The mask MDF/CDF are not
# built: the installer generates them from the installed Softing MDF/CDF.
set -e
cd "$(dirname "$0")"
ZIG="${ZIG:-zig}"
mkdir -p dist dist/x64 build

"$ZIG" cc -target x86-windows-gnu -O2 -shared -Wall \
	-o dist/PDUAPI_VAS6154_Shim.dll shim/shim.c shim/shim.def
# x64 has no stdcall name decoration: export the plain names.
sed -e 's/ = .*//' shim/shim.def > build/shim64.def
"$ZIG" cc -target x86_64-windows-gnu -O2 -shared -Wall \
	-o dist/x64/PDUAPI_VAS6154_Shim.dll shim/shim.c build/shim64.def shim/shim64.rc
rm -f dist/*.lib dist/*.pdb dist/x64/*.lib dist/x64/*.pdb

[ -f dist/vas6154shim.ini ] || cp shim/vas6154shim.ini dist/
[ -f dist/x64/vas6154shim.ini ] || cp shim/vas6154shim64.ini dist/x64/vas6154shim.ini

(cd tester && GOARCH=386 CGO_ENABLED=0 go build -ldflags "-H windowsgui" -o ../dist/VAS6154Mask.exe .)
(cd tester && GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-H windowsgui" -o ../dist/x64/VAS6154Mask64.exe .)
ls -la dist dist/x64
