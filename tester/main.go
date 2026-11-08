// VAS6154Mask: tests and installs the VAS6154 mask shim for a VAS5054A.
//
// Build one per D-PDU API bitness (see build.sh); each exe tests and installs
// the shim DLL of its own bitness next to it:
//
//	GOARCH=386   -> dist\VAS6154Mask.exe       (Softing D-PDU API, 32-bit ODIS)
//	GOARCH=amd64 -> distd\VAS6154Mask64.exe (VW D-PDU API, 64-bit ODIS)
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pRegisterClassExW = user32.NewProc("RegisterClassExW")
	pCreateWindowExW  = user32.NewProc("CreateWindowExW")
	pDefWindowProcW   = user32.NewProc("DefWindowProcW")
	pGetMessageW      = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessageW = user32.NewProc("DispatchMessageW")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pSendMessageW     = user32.NewProc("SendMessageW")
	pSetWindowTextW   = user32.NewProc("SetWindowTextW")
	pMoveWindow       = user32.NewProc("MoveWindow")
	pShowWindow       = user32.NewProc("ShowWindow")
	pLoadCursorW      = user32.NewProc("LoadCursorW")
	pMessageBoxW      = user32.NewProc("MessageBoxW")
	pEnableWindow     = user32.NewProc("EnableWindow")
	pInvalidateRect   = user32.NewProc("InvalidateRect")
	pGetSysColorBrush = user32.NewProc("GetSysColorBrush")
	pCreateFontW      = gdi32.NewProc("CreateFontW")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	pSetBkMode        = gdi32.NewProc("SetBkMode")
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pWaitForSingleObj = kernel32.NewProc("WaitForSingleObject")
	pCloseHandle      = kernel32.NewProc("CloseHandle")
	pShellExecuteExW  = shell32.NewProc("ShellExecuteExW")
)

const (
	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmSetFont        = 0x0030
	wmCommand        = 0x0111
	wmCtlColorStatic = 0x0138
	emSetSel         = 0x00B1
	emReplaceSel     = 0x00C2

	wsOverlappedWin = 0x00CF0000
	wsVisible       = 0x10000000
	wsChild         = 0x40000000
	wsVScroll       = 0x00200000
	wsBorder        = 0x00800000
	esMultiline     = 0x0004
	esAutoVScroll   = 0x0040
	esReadOnly      = 0x0800

	idTest      = 101
	idInstall   = 102
	idUninstall = 103
	idOpenLog   = 104
)

type wndClassEx struct {
	cbSize, style                    uint32
	lpfnWndProc                      uintptr
	cbClsExtra, cbWndExtra           int32
	hInstance, hIcon, hCursor, hbrBg uintptr
	lpszMenuName, lpszClassName      *uint16
	hIconSm                          uintptr
}

type msg struct {
	hwnd           uintptr
	message        uint32
	wParam, lParam uintptr
	time           uint32
	x, y           int32
}

type shellExecuteInfo struct {
	cbSize, fMask                            uint32
	hwnd                                     uintptr
	lpVerb, lpFile, lpParameters, lpDir      *uint16
	nShow                                    int32
	hInstApp, lpIDList                       uintptr
	lpClass                                  *uint16
	hkeyClass                                uintptr
	dwHotKey                                 uint32
	hIconOrMonitor, hProcess                 uintptr
}

var (
	hwndMain, hwndStatus, hwndLog uintptr
	buttons                       [4]uintptr
	statusColor                   uintptr = 0x606060
	dist                          distFiles
	exeDir                        string
	busy                          sync.Mutex
)

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func setStatus(text string, color uintptr) {
	statusColor = color
	pSetWindowTextW.Call(hwndStatus, uintptr(unsafe.Pointer(u16(text))))
	pInvalidateRect.Call(hwndStatus, 0, 1)
}

func logLine(format string, a ...any) {
	line := time.Now().Format("15:04:05.000 ") + fmt.Sprintf(format, a...) + "\r\n"
	pSendMessageW.Call(hwndLog, emSetSel, 0xFFFFFFFF, 0xFFFFFFFF)
	pSendMessageW.Call(hwndLog, emReplaceSel, 0, uintptr(unsafe.Pointer(u16(line))))
}

func enableButtons(on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	for _, b := range buttons {
		pEnableWindow.Call(b, v)
	}
}

func refreshInstallState() {
	on := installedAPIs()
	for _, api := range plat.apis {
		if slices.Contains(on, api) {
			logLine("ODIS (%s): %s currently points to the VAS6154 mask shim.", plat.name, api)
		} else {
			logLine("ODIS (%s): %s points to the original %s.", plat.name, api, plat.original)
		}
	}
}

// testMDF returns the MDF the MVCI server would use. On 32-bit it generates
// the mask MDF/CDF, on 64-bit it refreshes the shim's resource map from the VW MDFs.
func testMDF(log func(string, ...any)) string {
	mdf := apiMDF(plat, dist, plat.apis[0])
	if plat.ownMDF {
		b, err := os.ReadFile(plat.rootFile)
		if err == nil {
			err = writeMaskDescriptions(string(b), dist)
		}
		if err != nil {
			log("Mask MDF/CDF: %v", err)
		}
	} else {
		if err := writeResourceMap(plat, dist); err != nil {
			log("Resource map: %v", err)
		}
	}
	return mdf
}

func doTest() {
	if !busy.TryLock() {
		return
	}
	enableButtons(false)
	go func() {
		defer func() { enableButtons(true); busy.Unlock() }()
		// The D-PDU API keeps per-thread state; run the whole session on one OS thread.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		setStatus("Testing...", 0x606060)
		logLine("---- test start ----")
		r := runTest(dist.dll, testMDF(logLine), logLine)
		logLine("---- test end ----")
		if r.ok {
			setStatus(r.summary, 0x208020)
		} else {
			setStatus("FAILED - "+r.summary, 0x2020C0)
		}
	}()
}

// runElevated re-runs this exe as administrator with one argument and waits.
func runElevated(arg string) error {
	exe, _ := os.Executable()
	sei := shellExecuteInfo{
		fMask:        0x40, // SEE_MASK_NOCLOSEPROCESS
		hwnd:         hwndMain,
		lpVerb:       u16("runas"),
		lpFile:       u16(exe),
		lpParameters: u16(arg),
		nShow:        0,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	if r, _, err := pShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei))); r == 0 {
		return err
	}
	pWaitForSingleObj.Call(sei.hProcess, 0xFFFFFFFF)
	pCloseHandle.Call(sei.hProcess)
	return nil
}

func doElevated(arg, what string) {
	if !busy.TryLock() {
		return
	}
	enableButtons(false)
	go func() {
		defer func() { enableButtons(true); busy.Unlock() }()
		logLine("%s (administrator rights needed for %s)...", what, plat.rootFile)
		if err := runElevated(arg); err != nil {
			logLine("%s cancelled: %v", what, err)
			return
		}
		refreshInstallState()
		if arg == "-install" && len(installedAPIs()) > 0 {
			setStatus("Installed. In ODIS select the diagnostic interface VAS6154, then restart ODIS.", 0x208020)
		} else if arg == "-uninstall" && len(installedAPIs()) == 0 {
			setStatus("Original "+plat.original+" entries restored.", 0x606060)
		}
	}()
}

func layout(w, h int32) {
	const pad, bh = 10, 30
	bw := (w - pad*5) / 4
	for i, b := range buttons {
		pMoveWindow.Call(b, uintptr(pad+int32(i)*(bw+pad)), pad, uintptr(bw), bh, 1)
	}
	pMoveWindow.Call(hwndStatus, pad, pad*2+bh, uintptr(w-pad*2), 48, 1)
	top := int32(pad*3 + bh + 48)
	pMoveWindow.Call(hwndLog, pad, uintptr(top), uintptr(w-pad*2), uintptr(h-top-pad), 1)
}

func wndProc(hwnd uintptr, m uint32, wp, lp uintptr) uintptr {
	switch m {
	case wmSize:
		layout(int32(lp&0xFFFF), int32((lp>>16)&0xFFFF))
		return 0
	case wmCtlColorStatic:
		if lp == hwndStatus {
			pSetTextColor.Call(wp, statusColor)
			pSetBkMode.Call(wp, 1) // TRANSPARENT
			r, _, _ := pGetSysColorBrush.Call(15)
			return r
		}
	case wmCommand:
		switch wp & 0xFFFF {
		case idTest:
			doTest()
		case idInstall:
			doElevated("-install", "Installing into "+plat.users)
		case idUninstall:
			doElevated("-uninstall", "Restoring original")
		case idOpenLog:
			exec.Command("notepad.exe", filepath.Join(exeDir, "vas6154shim.log")).Start()
		}
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(m), wp, lp)
	return r
}

func msgBox(text string, flags uintptr) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16("VAS6154 Mask"))), flags)
}

func main() {
	exe, _ := os.Executable()
	exeDir = filepath.Dir(exe)
	dist = distPaths(exeDir)

	// Elevated helper modes, started by the GUI through "runas".
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "-install":
			err = install(dist)
		case "-uninstall":
			err = uninstall(dist)
		case "-selftest": // headless test, log written to the file given as next argument
			var out []string
			log := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
			r := runTest(dist.dll, testMDF(log), log)
			out = append(out, fmt.Sprintf("RESULT ok=%v: %s", r.ok, r.summary))
			if len(os.Args) > 2 {
				os.WriteFile(os.Args[2], []byte(strings.Join(out, "\r\n")+"\r\n"), 0o644)
			}
			if !r.ok {
				os.Exit(2)
			}
			return
		}
		if err != nil {
			msgBox(os.Args[1]+" failed:\n"+err.Error(), 0x10)
			os.Exit(1)
		}
		return
	}

	runtime.LockOSThread()
	hInst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, 32512)
	wc := wndClassEx{
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		hCursor:       cursor,
		hbrBg:         16, // COLOR_BTNFACE + 1
		lpszClassName: u16("VAS6154Mask"),
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwndMain, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16("VAS6154Mask"))),
		uintptr(unsafe.Pointer(u16("VAS6154 Mask for VAS5054A ("+plat.name+")"))), wsOverlappedWin|wsVisible,
		0x80000000, 0x80000000, 900, 600, 0, 0, hInst, 0)

	// Button font; small enough for "Install into ODIS + ContiTool" on a quarter-width button.
	uiFont, _, _ := pCreateFontW.Call(^uintptr(13), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
	boldFont, _, _ := pCreateFontW.Call(^uintptr(19), 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
	monoFont, _, _ := pCreateFontW.Call(^uintptr(14), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Consolas"))))

	labels := []string{"Run test", "Install into " + plat.users, "Restore original", "Open shim log"}
	for i, l := range labels {
		buttons[i], _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16("BUTTON"))),
			uintptr(unsafe.Pointer(u16(l))), wsChild|wsVisible, 0, 0, 0, 0, hwndMain, uintptr(idTest+i), hInst, 0)
		pSendMessageW.Call(buttons[i], wmSetFont, uiFont, 1)
	}
	hwndStatus, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16("STATIC"))),
		uintptr(unsafe.Pointer(u16("Plug in the VAS5054A, close ODIS, then press \"Run test\"."))),
		wsChild|wsVisible, 0, 0, 0, 0, hwndMain, 0, hInst, 0)
	pSendMessageW.Call(hwndStatus, wmSetFont, boldFont, 1)
	hwndLog, _, _ = pCreateWindowExW.Call(0x200, uintptr(unsafe.Pointer(u16("EDIT"))), 0,
		wsChild|wsVisible|wsVScroll|wsBorder|esMultiline|esAutoVScroll|esReadOnly,
		0, 0, 0, 0, hwndMain, 0, hInst, 0)
	pSendMessageW.Call(hwndLog, wmSetFont, monoFont, 1)
	pSendMessageW.Call(hwndLog, 0x00C5, 0, 0) // EM_LIMITTEXT: no limit

	layout(884, 561)
	pShowWindow.Call(hwndMain, 5)

	logLine("Shim: %s", dist.dll)
	for _, p := range []string{dist.dll, filepath.Join(exeDir, "vas6154shim.ini")} {
		if _, err := os.Stat(p); err != nil {
			logLine("MISSING: %s", p)
		}
	}
	refreshInstallState()

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
