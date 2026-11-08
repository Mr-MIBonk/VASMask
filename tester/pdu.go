package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"syscall"
	"unsafe"
)

// D-PDU API constants (ISO 22900-2, see shim/pdu_api.h).
const (
	pduStatusNoError = 0x00
	pduHandleUndef   = 0xFFFFFFFF
	pduObjtIoCtrl    = 0x8023
	pduModstReady    = 0x8060
	pduModstNotReady = 0x8061
	pduModstNotAvail = 0x8062
	pduModstAvail    = 0x8063
)

var pduErrNames = map[uintptr]string{
	0x00: "PDU_STATUS_NOERROR",
	0x01: "PDU_ERR_FCT_FAILED",
	0x11: "PDU_ERR_COMM_PC_TO_VCI_FAILED",
	0x20: "PDU_ERR_PDUAPI_NOT_CONSTRUCTED",
	0x21: "PDU_ERR_SHARING_VIOLATION",
	0x30: "PDU_ERR_RESOURCE_BUSY",
	0x50: "PDU_ERR_INVALID_PARAMETERS",
	0x60: "PDU_ERR_INVALID_HANDLE",
	0x61: "PDU_ERR_VALUE_NOT_SUPPORTED",
	0x62: "PDU_ERR_ID_NOT_SUPPORTED",
	0x80: "PDU_ERR_VOLTAGE_NOT_SUPPORTED",
	0xA3: "PDU_ERR_MODULE_NOT_CONNECTED",
	0xA4: "PDU_ERR_API_SW_OUT_OF_DATE",
	0xA5: "PDU_ERR_MODULE_FW_OUT_OF_DATE",
}

func pduErr(r uintptr) string {
	if n, ok := pduErrNames[r]; ok {
		return n
	}
	return fmt.Sprintf("0x%X", r)
}

func modStatus(s uint32) string {
	switch s {
	case pduModstReady:
		return "READY"
	case pduModstNotReady:
		return "NOT_READY"
	case pduModstNotAvail:
		return "NOT_AVAIL"
	case pduModstAvail:
		return "AVAIL"
	}
	return fmt.Sprintf("0x%X", s)
}

type moduleItem struct {
	ItemType    uint32
	NumEntries  uint32
	pModuleData uintptr
}

type moduleData struct {
	ModuleTypeId          uint32
	HMod                  uint32
	pVendorModuleName     uintptr
	pVendorAdditionalInfo uintptr
	ModuleStatus          uint32
}

type versionData struct {
	Part1, Part2    uint32
	HwSerialNumber  uint32
	HwName          [64]byte
	HwVersion       uint32
	HwDate          uint32
	HwInterface     uint32
	FwName          [64]byte
	FwVersion       uint32
	FwDate          uint32
	VendorName      [64]byte
	PDUApiSwName    [64]byte
	PDUApiSwVersion uint32
	PDUApiSwDate    uint32
}

type dataItem struct {
	ItemType uint32
	pData    uintptr
}

// pduAPI calls a D-PDU API DLL the same way the MVCI server does.
type pduAPI struct {
	dll   *syscall.DLL
	procs map[string]*syscall.Proc
}

func loadPDU(path string) (*pduAPI, error) {
	d, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, err
	}
	return &pduAPI{dll: d, procs: map[string]*syscall.Proc{}}, nil
}

func (p *pduAPI) call(name string, args ...uintptr) uintptr {
	pr, ok := p.procs[name]
	if !ok {
		var err error
		if pr, err = p.dll.FindProc(name); err != nil {
			return 0x01
		}
		p.procs[name] = pr
	}
	r, _, _ := pr.Call(args...)
	return r
}

func cstr(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

func goStr(p uintptr) string {
	if p == 0 {
		return ""
	}
	var b []byte
	for i := uintptr(0); i < 1024; i++ {
		c := *(*byte)(unsafe.Pointer(p + i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

func fixedStr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func ver(v uint32) string {
	return fmt.Sprintf("%d.%d.%d", v>>24, (v>>16)&0xFF, (v>>8)&0xFF)
}

// mdfModuleTypes reads ModuleTypeId -> SHORT_NAME from an MDF, which is how
// the MVCI server names the module types the D-PDU API reports.
func mdfModuleTypes(path string) map[uint32]string {
	out := map[uint32]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	re := regexp.MustCompile(`<MODULETYPE>\s*<ID>(\d+)</ID>\s*<SHORT_NAME>([^<]+)</SHORT_NAME>`)
	for _, m := range re.FindAllSubmatch(b, -1) {
		id, _ := strconv.Atoi(string(m[1]))
		out[uint32(id)] = string(m[2])
	}
	return out
}

type testResult struct {
	ok      bool
	summary string
}

// runTest walks through what ODIS does when it opens a VAS6154: construct the
// API, list modules, map the module type via the MDF, connect, read version
// and battery voltage.
func runTest(shimPath, mdfPath string, log func(string, ...any)) testResult {
	log("Loading shim: %s", shimPath)
	api, err := loadPDU(shimPath)
	if err != nil {
		return testResult{false, "Cannot load shim DLL: " + err.Error()}
	}
	types := mdfModuleTypes(mdfPath)
	log("MDF %s: %d module types", mdfPath, len(types))

	if r := api.call("PDUConstruct", uintptr(unsafe.Pointer(cstr(""))), 0); r != pduStatusNoError {
		return testResult{false, "PDUConstruct failed: " + pduErr(r) + "\r\nIs ODIS running? Close it and try again."}
	}
	log("PDUConstruct: OK")
	defer func() {
		log("PDUDestruct: %s", pduErr(api.call("PDUDestruct")))
	}()

	var list uintptr
	if r := api.call("PDUGetModuleIds", uintptr(unsafe.Pointer(&list))); r != pduStatusNoError || list == 0 {
		return testResult{false, "PDUGetModuleIds failed: " + pduErr(r)}
	}
	item := (*moduleItem)(unsafe.Pointer(list))
	log("PDUGetModuleIds: %d module(s)", item.NumEntries)
	if item.NumEntries == 0 {
		api.call("PDUDestroyItem", list)
		return testResult{false, "No VCI found. Is the VAS5054A plugged in?"}
	}
	mods := unsafe.Slice((*moduleData)(unsafe.Pointer(item.pModuleData)), item.NumEntries)
	pick := -1
	for i, m := range mods {
		tn := types[m.ModuleTypeId]
		log("  [%d] type %d = %q (MDF), name %q, info %q, status %s", i, m.ModuleTypeId, tn,
			goStr(m.pVendorModuleName), goStr(m.pVendorAdditionalInfo), modStatus(m.ModuleStatus))
		if pick < 0 && (tn == "VAS6154" || tn == "VAS6154A") {
			pick = i
		}
	}
	if pick < 0 {
		api.call("PDUDestroyItem", list)
		return testResult{false, "VCI found, but no module is seen as type VAS6154."}
	}
	m := mods[pick]
	hMod, name, typeName := m.HMod, goStr(m.pVendorModuleName), types[m.ModuleTypeId]
	api.call("PDUDestroyItem", list)

	if r := api.call("PDUModuleConnect", uintptr(hMod)); r != pduStatusNoError {
		return testResult{false, "PDUModuleConnect failed: " + pduErr(r)}
	}
	log("PDUModuleConnect(%d): OK", hMod)
	defer func() {
		log("PDUModuleDisconnect: %s", pduErr(api.call("PDUModuleDisconnect", uintptr(hMod))))
	}()

	var v versionData
	hw := "?"
	if r := api.call("PDUGetVersion", uintptr(hMod), uintptr(unsafe.Pointer(&v))); r == pduStatusNoError {
		hw = fixedStr(v.HwName[:])
		log("PDUGetVersion: hw %q serial %d, fw %q %s, vendor %q, api %q %s",
			hw, v.HwSerialNumber, fixedStr(v.FwName[:]), ver(v.FwVersion),
			fixedStr(v.VendorName[:]), fixedStr(v.PDUApiSwName[:]), ver(v.PDUApiSwVersion))
	} else {
		log("PDUGetVersion: %s", pduErr(r))
	}

	vbatt := "not read"
	var ioID uint32
	if r := api.call("PDUGetObjectId", pduObjtIoCtrl, uintptr(unsafe.Pointer(cstr("PDU_IOCTL_READ_VBATT"))),
		uintptr(unsafe.Pointer(&ioID))); r == pduStatusNoError {
		var out uintptr
		r = api.call("PDUIoCtl", uintptr(hMod), pduHandleUndef, uintptr(ioID), 0, uintptr(unsafe.Pointer(&out)))
		if r == pduStatusNoError && out != 0 {
			di := (*dataItem)(unsafe.Pointer(out))
			if di.pData != 0 {
				mv := *(*uint32)(unsafe.Pointer(di.pData))
				vbatt = fmt.Sprintf("%.2f V", float64(mv)/1000)
			}
			api.call("PDUDestroyItem", out)
		} else {
			vbatt = "IOCTL " + pduErr(r)
		}
	}
	log("Battery voltage (PDU_IOCTL_READ_VBATT through the real VCI): %s", vbatt)

	cable := "not read"
	if r := api.call("PDUGetObjectId", pduObjtIoCtrl, uintptr(unsafe.Pointer(cstr("PDU_IOCTL_GET_CABLE_ID"))),
		uintptr(unsafe.Pointer(&ioID))); r == pduStatusNoError {
		var out uintptr
		r = api.call("PDUIoCtl", uintptr(hMod), pduHandleUndef, uintptr(ioID), 0, uintptr(unsafe.Pointer(&out)))
		if r == pduStatusNoError && out != 0 {
			if di := (*dataItem)(unsafe.Pointer(out)); di.pData != 0 {
				cable = fmt.Sprint(*(*uint32)(unsafe.Pointer(di.pData)))
			}
			api.call("PDUDestroyItem", out)
		} else {
			cable = "IOCTL " + pduErr(r)
		}
	}
	log("Cable id (PDU_IOCTL_GET_CABLE_ID): %s", cable)

	return testResult{true, fmt.Sprintf(
		"WORKS - the VAS5054A is presented as a VAS6154 (%s)\r\n"+
			"Module type: %s    Name: %s    HW: %s    Battery: %s", plat.name, typeName, name, hw, vbatt)}
}
