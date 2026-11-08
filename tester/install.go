package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unsafe"
)

// platform describes the D-PDU API setup the shim is installed into. ODIS uses
// the 32-bit one (Softing API, own MDF/CDF) or the 64-bit one (VW API, which
// already knows the VAS6154 module type, so only the library is replaced).
type platform struct {
	name string
	// D-PDU API root file the MVCI server reads (HKLM\SOFTWARE[\WOW6432Node]\D-PDU API\Root File).
	rootFile string
	// PDU API short names in the root file through which ODIS selects the VAS6154.
	apis []string
	// Whether the MDF/CDF entries are replaced by the mask MDF/CDF too.
	ownMDF bool
	// Driver the APIs point to originally, shown in the GUI.
	original string
}

var plat32 = platform{
	name:     "32-bit",
	rootFile: `C:\ProgramData\D-PDU API\pdu_api_root.xml`,
	apis:     []string{"XS_D_PDU_API_OE", "XS_D_PDU_API_OS"},
	ownMDF:   true,
	original: "ACTIA VAS6154 driver",
}

var plat64 = platform{
	name:     "64-bit",
	rootFile: `C:\ProgramData\D-PDU API\rdf64.xml`,
	apis:     []string{"VW_D_PDU_API_OE", "VW_D_PDU_API_OS"}, // ODIS-E, ODIS-S
	original: "VW D-PDU API",
}

// plat is the platform matching this build: a 32-bit process can only load the
// 32-bit shim, a 64-bit process only the 64-bit one.
var plat = func() platform {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return plat64
	}
	return plat32
}()

// Backup taken before the first install; "Restore" puts it back.
func (p platform) backup() string { return p.rootFile + ".before-vas6154mask" }

type distFiles struct{ dll, ini, mdf, cdf, resMap string }

const shimName = "PDUAPI_VAS6154_Shim"

func distPaths(dir string) distFiles {
	return distFiles{
		dll:    filepath.Join(dir, shimName+".dll"),
		ini:    filepath.Join(dir, "vas6154shim.ini"),
		mdf:    filepath.Join(dir, "MDF_VAS6154_Mask.xml"),
		cdf:    filepath.Join(dir, "CDF_VAS6154_Mask.xml"),
		resMap: filepath.Join(dir, "vas6154map.ini"),
	}
}

// apiDLL is the shim the root file entry of api points to. On 64-bit every
// API gets its own copy: the shim reads its target from the ini section named
// after its DLL file, so ODIS-E and ODIS-S each keep their own VW D-PDU API.
func (f distFiles) apiDLL(p platform, api string) string {
	if p.ownMDF {
		return f.dll
	}
	return filepath.Join(filepath.Dir(f.dll), shimName+"_"+api+".dll")
}

func iniSection(dll string) string {
	return strings.TrimSuffix(filepath.Base(dll), filepath.Ext(dll))
}

func isShim(lib string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(lib)), strings.ToLower(shimName))
}

var (
	pGetPrivateProfileStringW   = kernel32.NewProc("GetPrivateProfileStringW")
	pWritePrivateProfileStringW = kernel32.NewProc("WritePrivateProfileStringW")
)

func iniGet(ini, section, key, def string) string {
	buf := make([]uint16, 1024)
	n, _, _ := pGetPrivateProfileStringW.Call(uintptr(unsafe.Pointer(u16(section))), uintptr(unsafe.Pointer(u16(key))),
		uintptr(unsafe.Pointer(u16(def))), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(u16(ini))))
	return syscall.UTF16ToString(buf[:n])
}

func iniSet(ini, section, key, value string) error {
	if r, _, err := pWritePrivateProfileStringW.Call(uintptr(unsafe.Pointer(u16(section))), uintptr(unsafe.Pointer(u16(key))),
		uintptr(unsafe.Pointer(u16(value))), uintptr(unsafe.Pointer(u16(ini)))); r == 0 {
		return fmt.Errorf("%s: %w", ini, err)
	}
	return nil
}

// originalLibrary is the real D-PDU API behind lib: lib itself, or for a shim
// the Target of its ini section (falling back to [shim]).
func originalLibrary(lib string) string {
	if !isShim(lib) {
		return lib
	}
	ini := filepath.Join(filepath.Dir(lib), "vas6154shim.ini")
	return iniGet(ini, iniSection(lib), "Target", iniGet(ini, "shim", "Target", ""))
}

func fileURI(p string) string {
	return "file:/" + strings.ReplaceAll(p, `\`, "/")
}

func uriPath(u string) string {
	return filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(u, "file:"), "/"))
}

var (
	apiBlock    = regexp.MustCompile(`(?s)<MVCI_PDU_API>.*?</MVCI_PDU_API>`)
	libraryFile = regexp.MustCompile(`<LIBRARY_FILE URI="([^"]*)"`)
)

// apiFile returns the path of a file entry (LIBRARY_FILE, MODULE_DESCRIPTION_FILE)
// of api in the root file xml, "" if api is not registered.
func apiFile(xml, api, tag string) string {
	for _, blk := range apiBlock.FindAllString(xml, -1) {
		if strings.Contains(blk, "<SHORT_NAME>"+api+"</SHORT_NAME>") {
			return blockFile(blk, tag)
		}
	}
	return ""
}

func blockFile(blk, tag string) string {
	if m := regexp.MustCompile(`<` + tag + ` URI="([^"]*)"`).FindStringSubmatch(blk); m != nil {
		return uriPath(m[1])
	}
	return ""
}

// maskEdits turn the Softing MDF/CDF into the mask MDF/CDF. They are applied
// on the user's own Softing files at install time, so none ship with the mask.
var maskEdits = map[string][2]string{
	"MODULE_DESCRIPTION_FILE": {"<SHORT_NAME>VAS5054</SHORT_NAME>", "<SHORT_NAME>VAS6154</SHORT_NAME>"},
	"CABLE_DESCRIPTION_FILE":  {"<SHORT_NAME>J1962_CARB</SHORT_NAME>", "<SHORT_NAME>CABLE_OBDII_VAS6154</SHORT_NAME>"},
}

// writeMaskDescriptions generates f.mdf and f.cdf from the MDF/CDF registered
// in the root file for the Softing D-PDU API the shim wraps (its ini Target).
func writeMaskDescriptions(xml string, f distFiles) error {
	target := iniGet(f.ini, "shim", "Target", "")
	if target == "" {
		return fmt.Errorf("%s: no Target", f.ini)
	}
	var blk string
	for _, b := range apiBlock.FindAllString(xml, -1) {
		if strings.EqualFold(filepath.Clean(blockFile(b, "LIBRARY_FILE")), filepath.Clean(target)) {
			blk = b
			break
		}
	}
	if blk == "" {
		return fmt.Errorf("Softing D-PDU API %s not registered in %s", target, plat32.rootFile)
	}
	for tag, out := range map[string]string{"MODULE_DESCRIPTION_FILE": f.mdf, "CABLE_DESCRIPTION_FILE": f.cdf} {
		src := blockFile(blk, tag)
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		e := maskEdits[tag]
		if n := strings.Count(string(b), e[0]); n != 1 {
			return fmt.Errorf("%s: %d occurrences of %s, want 1", src, n, e[0])
		}
		if err := os.WriteFile(out, []byte(strings.Replace(string(b), e[0], e[1], 1)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func patchRoot(xml string, p platform, f distFiles) (string, int) {
	n := 0
	out := apiBlock.ReplaceAllStringFunc(xml, func(b string) string {
		for _, name := range p.apis {
			if strings.Contains(b, "<SHORT_NAME>"+name+"</SHORT_NAME>") {
				n++
				b = libraryFile.ReplaceAllString(b, `<LIBRARY_FILE URI="`+fileURI(f.apiDLL(p, name))+`"`)
				if p.ownMDF {
					b = regexp.MustCompile(`<MODULE_DESCRIPTION_FILE URI="[^"]*"`).ReplaceAllString(b, `<MODULE_DESCRIPTION_FILE URI="`+fileURI(f.mdf)+`"`)
					b = regexp.MustCompile(`<CABLE_DESCRIPTION_FILE URI="[^"]*"`).ReplaceAllString(b, `<CABLE_DESCRIPTION_FILE URI="`+fileURI(f.cdf)+`"`)
				}
			}
		}
		return b
	})
	return out, n
}

// restoreRoot points every shim entry back to the D-PDU API it wraps.
func restoreRoot(xml string) (string, int) {
	n := 0
	out := apiBlock.ReplaceAllStringFunc(xml, func(b string) string {
		m := libraryFile.FindStringSubmatch(b)
		if m == nil || !isShim(uriPath(m[1])) {
			return b
		}
		if orig := originalLibrary(uriPath(m[1])); orig != "" {
			n++
			b = strings.Replace(b, m[0], `<LIBRARY_FILE URI="`+fileURI(orig)+`"`, 1)
		}
		return b
	})
	return out, n
}

// apiMDF returns the MDF the MVCI server uses for the masked API.
func apiMDF(p platform, f distFiles, api string) string {
	if p.ownMDF {
		return f.mdf
	}
	b, err := os.ReadFile(p.rootFile)
	if err != nil {
		return ""
	}
	return apiFile(string(b), api, "MODULE_DESCRIPTION_FILE")
}

// installedAPIs lists the APIs of plat whose root file entry points to a shim.
func installedAPIs() []string {
	b, err := os.ReadFile(plat.rootFile)
	if err != nil {
		return nil
	}
	var on []string
	for _, api := range plat.apis {
		if isShim(apiFile(string(b), api, "LIBRARY_FILE")) {
			on = append(on, api)
		}
	}
	return on
}

// installCopies gives every 64-bit API its own shim copy and records the real
// D-PDU API it wraps as Target in the copy's ini section.
func installCopies(xml string, f distFiles) error {
	dll, err := os.ReadFile(f.dll)
	if err != nil {
		return err
	}
	for _, api := range plat.apis {
		lib := apiFile(xml, api, "LIBRARY_FILE")
		if lib == "" {
			continue
		}
		target := originalLibrary(lib)
		if target == "" {
			return fmt.Errorf("%s: real D-PDU API behind %s unknown", api, lib)
		}
		cp := f.apiDLL(plat, api)
		if err := iniSet(f.ini, iniSection(cp), "Target", target); err != nil {
			return err
		}
		if err := os.WriteFile(cp, dll, 0o644); err != nil {
			return fmt.Errorf("%w (close ODIS first)", err)
		}
	}
	return nil
}

func install(f distFiles) error {
	if _, err := os.Stat(f.dll); err != nil {
		return fmt.Errorf("missing %s", f.dll)
	}
	if !plat.ownMDF {
		if err := writeResourceMap(plat, f); err != nil {
			return err
		}
	}
	b, err := os.ReadFile(plat.rootFile)
	if err != nil {
		return err
	}
	if plat.ownMDF {
		if err := writeMaskDescriptions(string(b), f); err != nil {
			return err
		}
	} else {
		if err := installCopies(string(b), f); err != nil {
			return err
		}
	}
	if _, err := os.Stat(plat.backup()); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(plat.backup(), b, 0o644); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	out, n := patchRoot(string(b), plat, f)
	if n == 0 {
		return fmt.Errorf("no %v entry in %s", plat.apis, plat.rootFile)
	}
	return os.WriteFile(plat.rootFile, []byte(out), 0o644)
}

func uninstall() error {
	if !plat.ownMDF {
		// Only the library entries were changed; restoring the whole backup
		// would also drop APIs registered after it was taken (e.g. ODIS-S).
		b, err := os.ReadFile(plat.rootFile)
		if err != nil {
			return err
		}
		out, _ := restoreRoot(string(b))
		if err := os.WriteFile(plat.rootFile, []byte(out), 0o644); err != nil {
			return err
		}
		os.Remove(plat.backup())
		return nil
	}
	b, err := os.ReadFile(plat.backup())
	if err != nil {
		return fmt.Errorf("no backup %s: %w", plat.backup(), err)
	}
	if err := os.WriteFile(plat.rootFile, b, 0o644); err != nil {
		return err
	}
	return os.Remove(plat.backup())
}

// IOCTLs of the VAS6154 module type that a J2534 module lacks; the shim
// answers them itself (name=value returns an UNUM32, name=ok returns nothing).
var fakeIoctls = []string{
	"PDU_IOCTL_GET_CABLE_ID=7002", // CABLE_ID of CABLE_OBDII_VAS6154 in CDF_VW.xml
	// J2534 cannot sense KL15; ODIS aborts the VAS6154A connect without it. 1 = ignition on.
	"PDU_IOCTL_READ_IGNITION_SENSE_STATE=1",
	"PDU_IOCTL_GET_ETH_SWITCH_STATE=0", // no DoIP activation line on a VAS5054A
	"PDU_IOCTL_SET_THROTTLE_STATE=ok",
	"PDU_IOCTL_VAS6154_REGISTER_UPDATE_PROGRESS=ok",
}

var mdfResource = regexp.MustCompile(`<RESOURCE EID="[^"]*">\s*<ID>(\d+)</ID>\s*<SHORT_NAME>([^<]+)</SHORT_NAME>`)

// resourceMap pairs every VAS6154 resource of the VW MDF with the J2534
// resource on the same pins and protocol (VAS6154_X <-> J2534_X).
func resourceMap(mdf string) (masked, real []string, err error) {
	b, err := os.ReadFile(mdf)
	if err != nil {
		return nil, nil, err
	}
	ids := map[string]string{}
	var names []string
	for _, m := range mdfResource.FindAllStringSubmatch(string(b), -1) {
		ids[m[2]] = m[1]
		names = append(names, m[2])
	}
	for _, n := range names {
		if !strings.Contains(n, "VAS6154_") {
			continue
		}
		if r, ok := ids[strings.Replace(n, "VAS6154_", "J2534_", 1)]; ok {
			masked = append(masked, ids[n])
			real = append(real, r)
		}
	}
	if len(masked) == 0 {
		return nil, nil, fmt.Errorf("no VAS6154/J2534 resource pairs in %s", mdf)
	}
	return masked, real, nil
}

// writeResourceMap writes vas6154map.ini, read by the 64-bit shim: [resources]
// from the MDF of the first API for the plain shim, plus one section per API
// for its shim copy.
func writeResourceMap(p platform, f distFiles) error {
	var sb strings.Builder
	sb.WriteString("; Generated by VAS6154Mask64.exe\r\n")
	n := 0
	for _, api := range p.apis {
		mdf := apiMDF(p, f, api)
		if mdf == "" {
			continue
		}
		masked, real, err := resourceMap(mdf)
		if err != nil {
			return err
		}
		sections := []string{"resources_" + iniSection(f.apiDLL(p, api))}
		if n == 0 {
			sections = append([]string{"resources"}, sections...)
		}
		for _, sec := range sections {
			sb.WriteString("[" + sec + "]\r\n; VAS6154 resource id=J2534 resource id, from " + mdf + "\r\n")
			for i := range masked {
				sb.WriteString(masked[i] + "=" + real[i] + "\r\n")
			}
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("none of %v registered in %s", p.apis, p.rootFile)
	}
	out := f.resMap
	sb.WriteString("[fake_ioctl]\r\n")
	for _, f := range fakeIoctls {
		sb.WriteString(f + "\r\n")
	}
	if old, err := os.ReadFile(out); err == nil && string(old) == sb.String() {
		return nil
	}
	return os.WriteFile(out, []byte(sb.String()), 0o644)
}
