package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchRootRealFile(t *testing.T) {
	b, err := os.ReadFile(plat32.rootFile)
	if err != nil {
		t.Skip(err)
	}
	if apiFile(string(b), plat32.apis[0], "LIBRARY_FILE") == "" {
		t.Skip("no", plat32.apis[0], "in", plat32.rootFile)
	}
	f := distPaths(`C:\mask`)
	out, n := patchRoot(string(b), plat32, f)
	if n != 2 {
		t.Fatalf("patched %d entries, want 2", n)
	}
	if c := strings.Count(out, fileURI(f.dll)); c != 2 {
		t.Fatalf("shim referenced %d times, want 2", c)
	}
	if !strings.Contains(out, "PDUAPI_SoftingAG_1.20.042.dll") {
		t.Fatal("Softing entries must stay untouched")
	}
	if strings.Contains(out, "PDUAPI_I+ME_ACTIA_XS.dll") {
		t.Fatal("ACTIA entries left unpatched")
	}
}

func TestPatchRoot64RealFile(t *testing.T) {
	b, err := os.ReadFile(plat64.rootFile)
	if err != nil {
		t.Skip(err)
	}
	f := distPaths(`C:\mask\x64`)
	out, n := patchRoot(string(b), plat64, f)
	if n != len(plat64.apis) {
		t.Fatalf("patched %d entries, want %d", n, len(plat64.apis))
	}
	if strings.Contains(out, "PDUAPI_VW.dll") {
		t.Fatal("VW library not replaced by the shim")
	}
	for _, api := range plat64.apis {
		if lib := apiFile(out, api, "LIBRARY_FILE"); lib != f.apiDLL(plat64, api) {
			t.Fatalf("%s points to %s", api, lib)
		}
	}
	if !strings.Contains(out, "MDF_VW.xml") || !strings.Contains(out, "CDF_VW.xml") {
		t.Fatal("VW MDF/CDF must stay")
	}
}

func TestRestoreRoot64(t *testing.T) {
	const root = `<MVCI_PDU_API_ROOT>
  <MVCI_PDU_API>
    <SHORT_NAME>VW_D_PDU_API_OE</SHORT_NAME>
    <LIBRARY_FILE URI="file:/C:/Program Files/VW_PDUAPI_OE/PDUAPI_VW.dll"/>
  </MVCI_PDU_API>
  <MVCI_PDU_API>
    <SHORT_NAME>VW_D_PDU_API_OS</SHORT_NAME>
    <LIBRARY_FILE URI="file:/C:/Program Files/VW_PDUAPI_OS/PDUAPI_VW.dll"/>
  </MVCI_PDU_API>
</MVCI_PDU_API_ROOT>`
	f := distPaths(t.TempDir())
	for _, api := range plat64.apis {
		cp := f.apiDLL(plat64, api)
		if err := iniSet(f.ini, iniSection(cp), "Target", apiFile(root, api, "LIBRARY_FILE")); err != nil {
			t.Fatal(err)
		}
	}
	patched, _ := patchRoot(root, plat64, f)
	out, n := restoreRoot(patched, "")
	if n != 2 || out != root {
		t.Fatalf("restored %d entries:\n%s", n, out)
	}

	// Shim folder registered in the root file was deleted: the installer's own ini knows the targets.
	gone, _ := patchRoot(root, plat64, distPaths(filepath.Join(t.TempDir(), "deleted")))
	out, n = restoreRoot(gone, f.ini)
	if n != 2 || out != root {
		t.Fatalf("restored %d entries from fallback ini:\n%s", n, out)
	}
}

func TestResourceMapVW(t *testing.T) {
	mdf := apiMDF(plat64, distPaths(`C:\mask\x64`), plat64.apis[0])
	if mdf == "" {
		t.Skip("no VW D-PDU API in", plat64.rootFile)
	}
	masked, real, err := resourceMap(mdf)
	if err != nil {
		t.Fatal(err)
	}
	// CAN1 (7) and K-line 1 (10) exist for J2534; CAN2 LS and K-line 2 do not.
	if len(masked) != 17 {
		t.Fatalf("%d resource pairs, want 17", len(masked))
	}
	for i := range masked {
		if masked[i] == real[i] {
			t.Fatalf("pair %d maps %s to itself", i, masked[i])
		}
	}
}

func TestWriteMaskDescriptions(t *testing.T) {
	b, err := os.ReadFile(plat32.rootFile)
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	f := distPaths(dir)
	src, _ := filepath.Abs(`..\shim\vas6154shim.ini`)
	target := iniGet(src, "shim", "Target", "")
	if _, err := os.Stat(target); err != nil {
		t.Skip("Softing D-PDU API not installed:", err)
	}
	if err := iniSet(f.ini, "shim", "Target", target); err != nil {
		t.Fatal(err)
	}
	if err := writeMaskDescriptions(string(b), f); err != nil {
		t.Fatal(err)
	}
	for out, want := range map[string]string{f.mdf: "<SHORT_NAME>VAS6154</SHORT_NAME>", f.cdf: "<SHORT_NAME>CABLE_OBDII_VAS6154</SHORT_NAME>"} {
		got, err := os.ReadFile(out)
		if err != nil || strings.Count(string(got), want) != 1 {
			t.Fatalf("%s: %v, want one %s", out, err, want)
		}
	}
}
