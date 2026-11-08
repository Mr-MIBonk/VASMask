/*
 * VAS6154 mask shim for the Softing VAS5054A D-PDU API.
 *
 * Exports the full ISO 22900-2 D-PDU API and forwards every call to the real
 * Softing PDU API DLL. Only identity data is rewritten (module name strings,
 * PDUGetVersion data) so the MVCI server sees a "VAS6154". Module type and
 * cable names are masked through the MDF/CDF registered next to this DLL.
 *
 * 64-bit build: wraps the VW D-PDU API (PDUAPI_VW.dll), which reaches the
 * VAS5054A as a J2534 module. With FromTypeId set, that module is reported
 * as module type VAS6154 of the VW MDF (see "type mask" below).
 *
 * Configuration: vas6154shim.ini next to the DLL, section [shim]. A section
 * named after the DLL file (e.g. [PDUAPI_VAS6154_Shim_VW_D_PDU_API_OS]) overrides
 * it, so copies of the shim can wrap different APIs (ODIS-E and ODIS-S).
 */
#include <windows.h>
#include <stdio.h>
#include <string.h>
#include <stdarg.h>
#include "pdu_api.h"

#undef UnlockResource /* winbase.h macro clashes with the PDU API name */

#define API __stdcall

typedef T_PDU_ERROR (API *tPDUConstruct)(CHAR8 *, void *);
typedef T_PDU_ERROR (API *tPDUDestruct)(void);
typedef T_PDU_ERROR (API *tPDUModuleConnect)(UNUM32);
typedef T_PDU_ERROR (API *tPDUModuleDisconnect)(UNUM32);
typedef T_PDU_ERROR (API *tPDUGetTimestamp)(UNUM32, UNUM32 *);
typedef T_PDU_ERROR (API *tPDUIoCtl)(UNUM32, UNUM32, UNUM32, PDU_DATA_ITEM *, PDU_DATA_ITEM **);
typedef T_PDU_ERROR (API *tPDUGetVersion)(UNUM32, PDU_VERSION_DATA *);
typedef T_PDU_ERROR (API *tPDUGetStatus)(UNUM32, UNUM32, UNUM32, T_PDU_STATUS *, UNUM32 *, UNUM32 *);
typedef T_PDU_ERROR (API *tPDUGetLastError)(UNUM32, UNUM32, T_PDU_ERR_EVT *, UNUM32 *, UNUM32 *, UNUM32 *);
typedef T_PDU_ERROR (API *tPDUGetResourceStatus)(PDU_RSC_STATUS_ITEM *);
typedef T_PDU_ERROR (API *tPDUCreateComLogicalLink)(UNUM32, PDU_RSC_DATA *, UNUM32, void *, UNUM32 *, PDU_FLAG_DATA *);
typedef T_PDU_ERROR (API *tPDUDestroyComLogicalLink)(UNUM32, UNUM32);
typedef T_PDU_ERROR (API *tPDUConnect)(UNUM32, UNUM32);
typedef T_PDU_ERROR (API *tPDUDisconnect)(UNUM32, UNUM32);
typedef T_PDU_ERROR (API *tPDULockResource)(UNUM32, UNUM32, UNUM32);
typedef T_PDU_ERROR (API *tPDUUnlockResource)(UNUM32, UNUM32, UNUM32);
typedef T_PDU_ERROR (API *tPDUGetComParam)(UNUM32, UNUM32, UNUM32, PDU_PARAM_ITEM **);
typedef T_PDU_ERROR (API *tPDUSetComParam)(UNUM32, UNUM32, PDU_PARAM_ITEM *);
typedef T_PDU_ERROR (API *tPDUStartComPrimitive)(UNUM32, UNUM32, UNUM32, UNUM32, UNUM8 *, PDU_COP_CTRL_DATA *, void *, UNUM32 *);
typedef T_PDU_ERROR (API *tPDUCancelComPrimitive)(UNUM32, UNUM32, UNUM32);
typedef T_PDU_ERROR (API *tPDUGetEventItem)(UNUM32, UNUM32, PDU_EVENT_ITEM **);
typedef T_PDU_ERROR (API *tPDUDestroyItem)(PDU_ITEM *);
typedef T_PDU_ERROR (API *tPDURegisterEventCallback)(UNUM32, UNUM32, CALLBACKFNC);
typedef T_PDU_ERROR (API *tPDUGetObjectId)(T_PDU_OBJT, CHAR8 *, UNUM32 *);
typedef T_PDU_ERROR (API *tPDUGetModuleIds)(PDU_MODULE_ITEM **);
typedef T_PDU_ERROR (API *tPDUGetResourceIds)(UNUM32, PDU_RSC_DATA *, PDU_RSC_ID_ITEM **);
typedef T_PDU_ERROR (API *tPDUGetConflictingResources)(UNUM32, PDU_MODULE_ITEM *, PDU_RSC_CONFLICT_ITEM **);
typedef T_PDU_ERROR (API *tPDUGetUniqueRespIdTable)(UNUM32, UNUM32, PDU_UNIQUE_RESP_ID_TABLE_ITEM **);
typedef T_PDU_ERROR (API *tPDUSetUniqueRespIdTable)(UNUM32, UNUM32, PDU_UNIQUE_RESP_ID_TABLE_ITEM *);

static struct {
	tPDUConstruct Construct;
	tPDUDestruct Destruct;
	tPDUModuleConnect ModuleConnect;
	tPDUModuleDisconnect ModuleDisconnect;
	tPDUGetTimestamp GetTimestamp;
	tPDUIoCtl IoCtl;
	tPDUGetVersion GetVersion;
	tPDUGetStatus GetStatus;
	tPDUGetLastError GetLastError;
	tPDUGetResourceStatus GetResourceStatus;
	tPDUCreateComLogicalLink CreateComLogicalLink;
	tPDUDestroyComLogicalLink DestroyComLogicalLink;
	tPDUConnect Connect;
	tPDUDisconnect Disconnect;
	tPDULockResource LockResource;
	tPDUUnlockResource UnlockResource;
	tPDUGetComParam GetComParam;
	tPDUSetComParam SetComParam;
	tPDUStartComPrimitive StartComPrimitive;
	tPDUCancelComPrimitive CancelComPrimitive;
	tPDUGetEventItem GetEventItem;
	tPDUDestroyItem DestroyItem;
	tPDURegisterEventCallback RegisterEventCallback;
	tPDUGetObjectId GetObjectId;
	tPDUGetModuleIds GetModuleIds;
	tPDUGetResourceIds GetResourceIds;
	tPDUGetConflictingResources GetConflictingResources;
	tPDUGetUniqueRespIdTable GetUniqueRespIdTable;
	tPDUSetUniqueRespIdTable SetUniqueRespIdTable;
} real;

static HMODULE self, target;
static INIT_ONCE initOnce = INIT_ONCE_STATIC_INIT;
static CRITICAL_SECTION logLock;
static FILE *logFile;

static char cfgIni[MAX_PATH], cfgSection[MAX_PATH];
static char cfgTarget[MAX_PATH];
static char cfgFrom[64], cfgTo[64];
static char cfgVendor[64], cfgSwName[64], cfgHwName[64];
static UNUM32 cfgTypeId; /* only modules of this MDF module type are masked; 0 = all */

/*
 * Type mask (64-bit VW D-PDU API): a module of type cfgFromType whose vendor
 * module name contains cfgMatch is reported as type cfgToType with name
 * cfgModuleName. Resource IDs of that module are translated through the
 * [resources] table of the map file (masked id = real id).
 */
static UNUM32 cfgFromType, cfgToType;
static char cfgMatch[64], cfgModuleName[256];
static int cfgHideDuplicates;

#define MAX_MASKED 8
static UNUM32 maskedMods[MAX_MASKED];
static int numMasked;

#define MAX_RSC 512
static UNUM32 rscMasked[MAX_RSC], rscReal[MAX_RSC];
static int numRsc;

/* IOCTLs the real module does not support but the masked type has. When the
 * real call fails they succeed, returning an UNUM32 value if one is given. */
#define MAX_FAKE 16
static struct {
	char name[96];
	UNUM32 id;
	int hasValue;
	UNUM32 value;
} fakes[MAX_FAKE];
static int numFakes;

/* Items handed out by the shim instead of the real API; PDUDestroyItem frees them. */
typedef struct owned {
	struct owned *next;
	void *pub;
	PDU_ITEM *orig;
} owned;
static owned *ownedList;
static CRITICAL_SECTION ownLock;

static void logf(const char *fmt, ...)
{
	if (!logFile)
		return;
	SYSTEMTIME t;
	GetLocalTime(&t);
	EnterCriticalSection(&logLock);
	fprintf(logFile, "%02d:%02d:%02d.%03d [%5lu] ", t.wHour, t.wMinute, t.wSecond, t.wMilliseconds, GetCurrentThreadId());
	va_list ap;
	va_start(ap, fmt);
	vfprintf(logFile, fmt, ap);
	va_end(ap);
	fputc('\n', logFile);
	fflush(logFile);
	LeaveCriticalSection(&logLock);
}

static void hex(char *out, size_t outLen, const UNUM8 *p, UNUM32 n)
{
	size_t o = 0;
	out[0] = 0;
	if (!p)
		return;
	for (UNUM32 i = 0; i < n && o + 4 < outLen; i++)
		o += sprintf(out + o, "%02X ", p[i]);
	if (o + 4 >= outLen && outLen > 4)
		strcpy(out + outLen - 4, "...");
}

/* Replace every occurrence of cfgFrom by cfgTo in place. Only same-length
 * replacement is done in place, because the buffer belongs to the real API. */
static void mask(char *s)
{
	size_t fl = strlen(cfgFrom);
	if (!s || !fl || fl != strlen(cfgTo))
		return;
	for (char *p = strstr(s, cfgFrom); p; p = strstr(p + fl, cfgFrom))
		memcpy(p, cfgTo, fl);
}

/* Same as mask, for fixed-size buffers we own; lengths may differ. */
static void maskBuf(char *s, size_t cap)
{
	char tmp[256];
	size_t fl = strlen(cfgFrom);
	if (!fl)
		return;
	char *p = strstr(s, cfgFrom);
	if (!p)
		return;
	snprintf(tmp, sizeof tmp, "%.*s%s%s", (int)(p - s), s, cfgTo, p + fl);
	snprintf(s, cap, "%s", tmp);
}

static int isMasked(UNUM32 hMod)
{
	for (int i = 0; i < numMasked; i++)
		if (maskedMods[i] == hMod)
			return 1;
	return 0;
}

static void addMasked(UNUM32 hMod)
{
	if (!isMasked(hMod) && numMasked < MAX_MASKED)
		maskedMods[numMasked++] = hMod;
}

/* Identity data is rewritten for every module in Softing mode, only for the
 * type-masked module in VW mode. */
static int identityMasked(UNUM32 hMod)
{
	return !cfgFromType || isMasked(hMod);
}

static UNUM32 rscToReal(UNUM32 id)
{
	for (int i = 0; i < numRsc; i++)
		if (rscMasked[i] == id)
			return rscReal[i];
	return id;
}

static UNUM32 rscToMasked(UNUM32 id)
{
	for (int i = 0; i < numRsc; i++)
		if (rscReal[i] == id)
			return rscMasked[i];
	return id;
}

static void own(void *pub, PDU_ITEM *orig)
{
	owned *o = calloc(1, sizeof *o);
	o->pub = pub;
	o->orig = orig;
	EnterCriticalSection(&ownLock);
	o->next = ownedList;
	ownedList = o;
	LeaveCriticalSection(&ownLock);
}

/* Returns 1 and releases the item if it was handed out by the shim. */
static int disown(void *pub)
{
	owned **pp, *o = NULL;
	EnterCriticalSection(&ownLock);
	for (pp = &ownedList; *pp; pp = &(*pp)->next) {
		if ((*pp)->pub == pub) {
			o = *pp;
			*pp = o->next;
			break;
		}
	}
	LeaveCriticalSection(&ownLock);
	if (!o)
		return 0;
	if (o->orig && real.DestroyItem)
		real.DestroyItem(o->orig);
	free(o->pub);
	free(o);
	return 1;
}

/* [shim] value of key, overridden by the DLL's own section. */
static void cfgStr(const char *key, const char *def, char *out, DWORD n)
{
	char tmp[MAX_PATH];
	GetPrivateProfileStringA("shim", key, def, tmp, sizeof tmp, cfgIni);
	GetPrivateProfileStringA(cfgSection, key, tmp, out, n, cfgIni);
}

static UNUM32 cfgInt(const char *key, int def)
{
	return GetPrivateProfileIntA(cfgSection, key, GetPrivateProfileIntA("shim", key, def, cfgIni), cfgIni);
}

static void loadMap(const char *path)
{
	static char buf[64 * 1024];
	char sect[MAX_PATH + 16];
	/* Resource ids of the MDF of this DLL's API, else the common table. */
	snprintf(sect, sizeof sect, "resources_%s", cfgSection);
	DWORD n = GetPrivateProfileSectionA(sect, buf, sizeof buf, path);
	if (!n)
		n = GetPrivateProfileSectionA("resources", buf, sizeof buf, path);
	for (char *p = buf; p < buf + n && *p && numRsc < MAX_RSC; p += strlen(p) + 1) {
		unsigned long m, r;
		if (sscanf(p, "%lu=%lu", &m, &r) == 2) {
			rscMasked[numRsc] = m;
			rscReal[numRsc++] = r;
		}
	}
	n = GetPrivateProfileSectionA("fake_ioctl", buf, sizeof buf, path);
	for (char *p = buf; p < buf + n && *p && numFakes < MAX_FAKE; p += strlen(p) + 1) {
		char *eq = strchr(p, '=');
		if (!eq)
			continue;
		snprintf(fakes[numFakes].name, sizeof fakes[0].name, "%.*s", (int)(eq - p), p);
		fakes[numFakes].id = PDU_ID_UNDEF;
		fakes[numFakes].hasValue = _stricmp(eq + 1, "ok") != 0;
		fakes[numFakes].value = strtoul(eq + 1, NULL, 0);
		numFakes++;
	}
	logf("map %s: %d resource pairs, %d fake IOCTLs", path, numRsc, numFakes);
}

/* IOCTL ids are only known once the API is constructed. */
static void resolveFakes(void)
{
	for (int i = 0; i < numFakes; i++) {
		UNUM32 id = PDU_ID_UNDEF;
		if (real.GetObjectId(PDU_OBJT_IO_CTRL, fakes[i].name, &id) == PDU_STATUS_NOERROR)
			fakes[i].id = id;
		logf("  fake IOCTL %s id=%lu", fakes[i].name, (unsigned long)fakes[i].id);
	}
}

/* Type mask of a module list: the caller gets a copy (strings of different
 * length cannot be written into the real API's buffers); the real list is
 * released when the caller destroys the copy. */
#define NAME_LEN 256
static void maskModules(PDU_MODULE_ITEM **list)
{
	PDU_MODULE_ITEM *in = *list;
	UNUM32 n = in->NumEntries;
	char *blk = calloc(1, sizeof(PDU_MODULE_ITEM) + n * (sizeof(PDU_MODULE_DATA) + NAME_LEN));
	if (!blk)
		return;
	PDU_MODULE_ITEM *out = (PDU_MODULE_ITEM *)blk;
	PDU_MODULE_DATA *md = (PDU_MODULE_DATA *)(out + 1);
	char *names = (char *)(md + n);
	out->ItemType = in->ItemType;
	out->pModuleData = md;

	for (UNUM32 i = 0; i < n; i++) {
		PDU_MODULE_DATA *m = &in->pModuleData[i];
		const char *name = m->pVendorModuleName ? m->pVendorModuleName : "";
		PDU_MODULE_DATA *d = &md[out->NumEntries];
		if (m->ModuleTypeId != cfgFromType || !cfgMatch[0] || !strstr(name, cfgMatch)) {
			*d = *m;
			out->NumEntries++;
			logf("  module[%lu] type=%lu hMod=%lu status=0x%lX name '%s'", (unsigned long)i,
				(unsigned long)m->ModuleTypeId, (unsigned long)m->hMod, (unsigned long)m->ModuleStatus, name);
			continue;
		}
		/* The same VCI can be registered several times for J2534 (e.g. Softing and SRT). */
		int dup = 0;
		for (UNUM32 j = 0; cfgHideDuplicates && j < i && !dup; j++) {
			PDU_MODULE_DATA *p = &in->pModuleData[j];
			dup = p->ModuleTypeId == cfgFromType && p->pVendorModuleName && !strcmp(p->pVendorModuleName, name);
		}
		if (dup) {
			logf("  module[%lu] hMod=%lu '%s' hidden (duplicate)", (unsigned long)i, (unsigned long)m->hMod, name);
			continue;
		}
		*d = *m;
		out->NumEntries++;
		char *s = names + i * NAME_LEN;
		if (cfgModuleName[0]) {
			/* {serial} = what follows '#' in the real module name, e.g. VAS5054A#012345678 */
			char serial[64] = "";
			const char *h = strchr(name, '#');
			if (h)
				sscanf(h + 1, "%63[^' ]", serial);
			const char *ph = strstr(cfgModuleName, "{serial}");
			if (ph)
				snprintf(s, NAME_LEN, "%.*s%s%s", (int)(ph - cfgModuleName), cfgModuleName, serial, ph + 8);
			else
				snprintf(s, NAME_LEN, "%s", cfgModuleName);
		} else {
			snprintf(s, NAME_LEN, "%s", name);
			maskBuf(s, NAME_LEN);
		}
		d->ModuleTypeId = cfgToType;
		d->pVendorModuleName = s;
		addMasked(m->hMod);
		logf("  module[%lu] type=%lu hMod=%lu status=0x%lX name '%s' -> type=%lu '%s'", (unsigned long)i,
			(unsigned long)m->ModuleTypeId, (unsigned long)m->hMod, (unsigned long)m->ModuleStatus, name,
			(unsigned long)cfgToType, s);
	}
	own(out, (PDU_ITEM *)in);
	*list = out;
}

static BOOL CALLBACK init(PINIT_ONCE once, PVOID param, PVOID *ctx)
{
	char dir[MAX_PATH], logPath[MAX_PATH];
	(void)once; (void)param; (void)ctx;

	GetModuleFileNameA(self, dir, MAX_PATH);
	char *slash = strrchr(dir, '\\');
	if (slash)
		*slash = 0;
	snprintf(cfgIni, MAX_PATH, "%s\\vas6154shim.ini", dir);
	snprintf(cfgSection, MAX_PATH, "%s", slash ? slash + 1 : dir);
	char *dot = strrchr(cfgSection, '.');
	if (dot)
		*dot = 0;

	cfgStr("Target",
		"C:\\Program Files (x86)\\Softing\\D-PDU API\\1.20.042\\VeCom\\PDUAPI_SoftingAG_1.20.042.dll",
		cfgTarget, sizeof cfgTarget);
	cfgStr("FromName", "VAS5054", cfgFrom, sizeof cfgFrom);
	cfgStr("ToName", "VAS6154", cfgTo, sizeof cfgTo);
	cfgStr("VendorName", "", cfgVendor, sizeof cfgVendor);
	cfgStr("PDUApiSwName", "", cfgSwName, sizeof cfgSwName);
	cfgStr("HwName", "", cfgHwName, sizeof cfgHwName);
	cfgTypeId = cfgInt("MaskModuleTypeId", 531);
	cfgFromType = cfgInt("FromTypeId", 0);
	cfgToType = cfgInt("ToTypeId", 0);
	cfgStr("MatchName", "", cfgMatch, sizeof cfgMatch);
	cfgStr("ModuleName", "", cfgModuleName, sizeof cfgModuleName);
	cfgHideDuplicates = cfgInt("HideDuplicates", 1);
	InitializeCriticalSection(&ownLock);

	InitializeCriticalSection(&logLock);
	if (cfgInt("Log", 1)) {
		cfgStr("LogFile", "", logPath, sizeof logPath);
		if (!logPath[0])
			snprintf(logPath, MAX_PATH, "%s\\vas6154shim.log", dir);
		logFile = fopen(logPath, "a");
	}
	logf("==== VAS6154 shim loaded (%d-bit), pid %lu, %s, target %s, mask '%s' -> '%s'",
		(int)sizeof(void *) * 8, GetCurrentProcessId(), cfgSection, cfgTarget, cfgFrom, cfgTo);
	if (cfgFromType) {
		char map[MAX_PATH];
		logf("type mask: type %lu with name '*%s*' -> type %lu '%s'", (unsigned long)cfgFromType,
			cfgMatch, (unsigned long)cfgToType, cfgModuleName);
		snprintf(map, MAX_PATH, "%s\\vas6154map.ini", dir);
		loadMap(map);
	}

	/* Let the Softing DLL find its own dependencies in its folder. */
	target = LoadLibraryExA(cfgTarget, NULL, LOAD_WITH_ALTERED_SEARCH_PATH);
	if (!target) {
		logf("LoadLibrary target failed: %lu", GetLastError());
		return TRUE;
	}
#define RESOLVE(name) real.name = (void *)GetProcAddress(target, "PDU" #name); \
	if (!real.name) logf("missing export PDU" #name);
	RESOLVE(Construct) RESOLVE(Destruct) RESOLVE(ModuleConnect) RESOLVE(ModuleDisconnect)
	RESOLVE(GetTimestamp) RESOLVE(IoCtl) RESOLVE(GetVersion) RESOLVE(GetStatus)
	RESOLVE(GetLastError) RESOLVE(GetResourceStatus) RESOLVE(CreateComLogicalLink)
	RESOLVE(DestroyComLogicalLink) RESOLVE(Connect) RESOLVE(Disconnect) RESOLVE(LockResource)
	RESOLVE(UnlockResource) RESOLVE(GetComParam) RESOLVE(SetComParam) RESOLVE(StartComPrimitive)
	RESOLVE(CancelComPrimitive) RESOLVE(GetEventItem) RESOLVE(DestroyItem)
	RESOLVE(RegisterEventCallback) RESOLVE(GetObjectId) RESOLVE(GetModuleIds)
	RESOLVE(GetResourceIds) RESOLVE(GetConflictingResources) RESOLVE(GetUniqueRespIdTable)
	RESOLVE(SetUniqueRespIdTable)
#undef RESOLVE
	return TRUE;
}

#define ENSURE(fn) \
	InitOnceExecuteOnce(&initOnce, init, NULL, NULL); \
	if (!real.fn) return PDU_ERR_FCT_FAILED;

BOOL WINAPI DllMain(HINSTANCE h, DWORD reason, LPVOID r)
{
	(void)r;
	if (reason == DLL_PROCESS_ATTACH) {
		self = h;
		DisableThreadLibraryCalls(h);
	}
	return TRUE;
}

/* ---------------- identity-rewriting calls ---------------- */

T_PDU_ERROR API PDUConstruct(CHAR8 *opt, void *tag)
{
	ENSURE(Construct)
	T_PDU_ERROR e = real.Construct(opt, tag);
	logf("PDUConstruct(opt='%s') = 0x%lX", opt ? opt : "(null)", (unsigned long)e);
	if (e == PDU_STATUS_NOERROR && cfgFromType)
		resolveFakes();
	return e;
}

T_PDU_ERROR API PDUGetModuleIds(PDU_MODULE_ITEM **list)
{
	ENSURE(GetModuleIds)
	T_PDU_ERROR e = real.GetModuleIds(list);
	logf("PDUGetModuleIds = 0x%lX", (unsigned long)e);
	if (e == PDU_STATUS_NOERROR && list && *list && cfgFromType) {
		maskModules(list);
	} else if (e == PDU_STATUS_NOERROR && list && *list) {
		for (UNUM32 i = 0; i < (*list)->NumEntries; i++) {
			PDU_MODULE_DATA *m = &(*list)->pModuleData[i];
			char before[160];
			snprintf(before, sizeof before, "%s", m->pVendorModuleName ? m->pVendorModuleName : "");
			if (!cfgTypeId || m->ModuleTypeId == cfgTypeId) {
				mask(m->pVendorModuleName);
				mask(m->pVendorAdditionalInfo);
			}
			logf("  module[%lu] type=%lu hMod=%lu status=0x%lX name '%s' -> '%s' info='%s'",
				(unsigned long)i, (unsigned long)m->ModuleTypeId, (unsigned long)m->hMod,
				(unsigned long)m->ModuleStatus, before,
				m->pVendorModuleName ? m->pVendorModuleName : "",
				m->pVendorAdditionalInfo ? m->pVendorAdditionalInfo : "");
		}
	}
	return e;
}

T_PDU_ERROR API PDUGetVersion(UNUM32 hMod, PDU_VERSION_DATA *v)
{
	ENSURE(GetVersion)
	T_PDU_ERROR e = real.GetVersion(hMod, v);
	if (e == PDU_STATUS_NOERROR && v && identityMasked(hMod)) {
		logf("PDUGetVersion(hMod=%lu) real: hw='%s' fw='%s' vendor='%s' sw='%s' serial=%lu",
			(unsigned long)hMod, v->HwName, v->FwName, v->VendorName, v->PDUApiSwName,
			(unsigned long)v->HwSerialNumber);
		maskBuf(v->HwName, sizeof v->HwName);
		if (cfgHwName[0])
			snprintf(v->HwName, sizeof v->HwName, "%s", cfgHwName);
		if (cfgVendor[0])
			snprintf(v->VendorName, sizeof v->VendorName, "%s", cfgVendor);
		if (cfgSwName[0])
			snprintf(v->PDUApiSwName, sizeof v->PDUApiSwName, "%s", cfgSwName);
		logf("  masked: hw='%s' vendor='%s' sw='%s'", v->HwName, v->VendorName, v->PDUApiSwName);
	} else {
		logf("PDUGetVersion(hMod=%lu) = 0x%lX", (unsigned long)hMod, (unsigned long)e);
	}
	return e;
}

T_PDU_ERROR API PDUGetObjectId(T_PDU_OBJT type, CHAR8 *name, UNUM32 *id)
{
	ENSURE(GetObjectId)
	T_PDU_ERROR e = real.GetObjectId(type, name, id);
	logf("PDUGetObjectId(type=0x%X, '%s') = 0x%lX id=%lu", type, name ? name : "(null)",
		(unsigned long)e, id ? (unsigned long)*id : 0UL);
	return e;
}

/* ---------------- logged pass-through calls ---------------- */

T_PDU_ERROR API PDUDestruct(void)
{
	ENSURE(Destruct)
	T_PDU_ERROR e = real.Destruct();
	logf("PDUDestruct = 0x%lX", (unsigned long)e);
	return e;
}

T_PDU_ERROR API PDUModuleConnect(UNUM32 hMod)
{
	ENSURE(ModuleConnect)
	T_PDU_ERROR e = real.ModuleConnect(hMod);
	logf("PDUModuleConnect(hMod=%lu) = 0x%lX", (unsigned long)hMod, (unsigned long)e);
	return e;
}

T_PDU_ERROR API PDUModuleDisconnect(UNUM32 hMod)
{
	ENSURE(ModuleDisconnect)
	T_PDU_ERROR e = real.ModuleDisconnect(hMod);
	logf("PDUModuleDisconnect(hMod=%lu) = 0x%lX", (unsigned long)hMod, (unsigned long)e);
	return e;
}

T_PDU_ERROR API PDUGetTimestamp(UNUM32 hMod, UNUM32 *ts)
{
	ENSURE(GetTimestamp)
	return real.GetTimestamp(hMod, ts);
}

T_PDU_ERROR API PDUIoCtl(UNUM32 hMod, UNUM32 hCLL, UNUM32 cmd, PDU_DATA_ITEM *in, PDU_DATA_ITEM **out)
{
	ENSURE(IoCtl)
	T_PDU_ERROR e = real.IoCtl(hMod, hCLL, cmd, in, out);
	logf("PDUIoCtl(hMod=%lu, hCLL=%lu, cmd=%lu, in=0x%lX) = 0x%lX", (unsigned long)hMod,
		(unsigned long)hCLL, (unsigned long)cmd, in ? (unsigned long)in->ItemType : 0UL, (unsigned long)e);
	if (e == PDU_STATUS_NOERROR || !isMasked(hMod))
		return e;
	for (int i = 0; i < numFakes; i++) {
		if (fakes[i].id != cmd)
			continue;
		if (out)
			*out = NULL;
		if (fakes[i].hasValue && out) {
			struct { PDU_DATA_ITEM item; UNUM32 value; } *r = calloc(1, sizeof *r);
			if (!r)
				return e;
			r->item.ItemType = PDU_IT_IO_UNUM32;
			r->item.pData = &r->value;
			r->value = fakes[i].value;
			own(r, NULL);
			*out = &r->item;
		}
		logf("  faked %s: OK value=%lu", fakes[i].name, (unsigned long)fakes[i].value);
		return PDU_STATUS_NOERROR;
	}
	return e;
}

T_PDU_ERROR API PDUGetStatus(UNUM32 hMod, UNUM32 hCLL, UNUM32 hCoP, T_PDU_STATUS *st, UNUM32 *ts, UNUM32 *extra)
{
	ENSURE(GetStatus)
	return real.GetStatus(hMod, hCLL, hCoP, st, ts, extra);
}

T_PDU_ERROR API PDUGetLastError(UNUM32 hMod, UNUM32 hCLL, T_PDU_ERR_EVT *code, UNUM32 *hCoP, UNUM32 *ts, UNUM32 *extra)
{
	ENSURE(GetLastError)
	T_PDU_ERROR e = real.GetLastError(hMod, hCLL, code, hCoP, ts, extra);
	logf("PDUGetLastError(hMod=%lu, hCLL=%lu) = 0x%lX code=0x%lX extra=0x%lX", (unsigned long)hMod,
		(unsigned long)hCLL, (unsigned long)e, code ? (unsigned long)*code : 0UL, extra ? (unsigned long)*extra : 0UL);
	return e;
}

T_PDU_ERROR API PDUGetResourceStatus(PDU_RSC_STATUS_ITEM *s)
{
	ENSURE(GetResourceStatus)
	if (!s || !s->pResourceStatusData)
		return real.GetResourceStatus(s);
	for (UNUM32 i = 0; i < s->NumEntries; i++)
		if (isMasked(s->pResourceStatusData[i].hMod))
			s->pResourceStatusData[i].ResourceId = rscToReal(s->pResourceStatusData[i].ResourceId);
	T_PDU_ERROR e = real.GetResourceStatus(s);
	for (UNUM32 i = 0; i < s->NumEntries; i++)
		if (isMasked(s->pResourceStatusData[i].hMod))
			s->pResourceStatusData[i].ResourceId = rscToMasked(s->pResourceStatusData[i].ResourceId);
	return e;
}

T_PDU_ERROR API PDUCreateComLogicalLink(UNUM32 hMod, PDU_RSC_DATA *rsc, UNUM32 rscId, void *tag, UNUM32 *hCLL, PDU_FLAG_DATA *flags)
{
	ENSURE(CreateComLogicalLink)
	UNUM32 realId = isMasked(hMod) ? rscToReal(rscId) : rscId;
	T_PDU_ERROR e = real.CreateComLogicalLink(hMod, rsc, realId, tag, hCLL, flags);
	logf("PDUCreateComLogicalLink(hMod=%lu, bus=%lu, prot=%lu, rscId=%lu->%lu) = 0x%lX hCLL=%lu",
		(unsigned long)hMod, rsc ? (unsigned long)rsc->BusTypeId : 0UL, rsc ? (unsigned long)rsc->ProtocolId : 0UL,
		(unsigned long)rscId, (unsigned long)realId, (unsigned long)e, hCLL ? (unsigned long)*hCLL : 0UL);
	return e;
}

T_PDU_ERROR API PDUDestroyComLogicalLink(UNUM32 hMod, UNUM32 hCLL)
{
	ENSURE(DestroyComLogicalLink)
	T_PDU_ERROR e = real.DestroyComLogicalLink(hMod, hCLL);
	logf("PDUDestroyComLogicalLink(hMod=%lu, hCLL=%lu) = 0x%lX", (unsigned long)hMod, (unsigned long)hCLL, (unsigned long)e);
	return e;
}

T_PDU_ERROR API PDUConnect(UNUM32 hMod, UNUM32 hCLL)
{
	ENSURE(Connect)
	T_PDU_ERROR e = real.Connect(hMod, hCLL);
	logf("PDUConnect(hMod=%lu, hCLL=%lu) = 0x%lX", (unsigned long)hMod, (unsigned long)hCLL, (unsigned long)e);
	return e;
}

T_PDU_ERROR API PDUDisconnect(UNUM32 hMod, UNUM32 hCLL)
{
	ENSURE(Disconnect)
	T_PDU_ERROR e = real.Disconnect(hMod, hCLL);
	logf("PDUDisconnect(hMod=%lu, hCLL=%lu) = 0x%lX", (unsigned long)hMod, (unsigned long)hCLL, (unsigned long)e);
	return e;
}

T_PDU_ERROR API PDULockResource(UNUM32 hMod, UNUM32 hCLL, UNUM32 mask)
{
	ENSURE(LockResource)
	return real.LockResource(hMod, hCLL, mask);
}

T_PDU_ERROR API PDUUnlockResource(UNUM32 hMod, UNUM32 hCLL, UNUM32 mask)
{
	ENSURE(UnlockResource)
	return real.UnlockResource(hMod, hCLL, mask);
}

T_PDU_ERROR API PDUGetComParam(UNUM32 hMod, UNUM32 hCLL, UNUM32 id, PDU_PARAM_ITEM **item)
{
	ENSURE(GetComParam)
	T_PDU_ERROR e = real.GetComParam(hMod, hCLL, id, item);
	if (e != PDU_STATUS_NOERROR)
		logf("PDUGetComParam(hCLL=%lu, id=%lu) = 0x%lX", (unsigned long)hCLL, (unsigned long)id, (unsigned long)e);
	return e;
}

T_PDU_ERROR API PDUSetComParam(UNUM32 hMod, UNUM32 hCLL, PDU_PARAM_ITEM *item)
{
	ENSURE(SetComParam)
	T_PDU_ERROR e = real.SetComParam(hMod, hCLL, item);
	if (e != PDU_STATUS_NOERROR)
		logf("PDUSetComParam(hCLL=%lu, id=%lu) = 0x%lX", (unsigned long)hCLL,
			item ? (unsigned long)item->ComParamId : 0UL, (unsigned long)e);
	return e;
}

T_PDU_ERROR API PDUStartComPrimitive(UNUM32 hMod, UNUM32 hCLL, UNUM32 type, UNUM32 size, UNUM8 *data, PDU_COP_CTRL_DATA *ctrl, void *tag, UNUM32 *hCoP)
{
	char h[200];
	ENSURE(StartComPrimitive)
	T_PDU_ERROR e = real.StartComPrimitive(hMod, hCLL, type, size, data, ctrl, tag, hCoP);
	hex(h, sizeof h, data, size);
	logf("PDUStartComPrimitive(hCLL=%lu, type=0x%lX, data=[%s]) = 0x%lX hCoP=%lu", (unsigned long)hCLL,
		(unsigned long)type, h, (unsigned long)e, hCoP ? (unsigned long)*hCoP : 0UL);
	return e;
}

T_PDU_ERROR API PDUCancelComPrimitive(UNUM32 hMod, UNUM32 hCLL, UNUM32 hCoP)
{
	ENSURE(CancelComPrimitive)
	return real.CancelComPrimitive(hMod, hCLL, hCoP);
}

T_PDU_ERROR API PDUGetEventItem(UNUM32 hMod, UNUM32 hCLL, PDU_EVENT_ITEM **item)
{
	char h[200];
	ENSURE(GetEventItem)
	T_PDU_ERROR e = real.GetEventItem(hMod, hCLL, item);
	if (e == PDU_STATUS_NOERROR && item && *item) {
		PDU_EVENT_ITEM *ev = *item;
		if (ev->ItemType == PDU_IT_RESULT && ev->pData) {
			PDU_RESULT_DATA *r = ev->pData;
			hex(h, sizeof h, r->pDataBytes, r->NumDataBytes);
			logf("  event hCLL=%lu RESULT hCoP=%lu [%s]", (unsigned long)hCLL, (unsigned long)ev->hCop, h);
		} else if (ev->ItemType == PDU_IT_ERROR && ev->pData) {
			PDU_ERROR_DATA *d = ev->pData;
			logf("  event hCLL=%lu ERROR hCoP=%lu code=0x%lX extra=0x%lX", (unsigned long)hCLL,
				(unsigned long)ev->hCop, (unsigned long)d->ErrorCodeId, (unsigned long)d->ExtraErrorInfoId);
		}
	}
	return e;
}

T_PDU_ERROR API PDUDestroyItem(PDU_ITEM *item)
{
	ENSURE(DestroyItem)
	if (item && disown(item))
		return PDU_STATUS_NOERROR;
	return real.DestroyItem(item);
}

T_PDU_ERROR API PDURegisterEventCallback(UNUM32 hMod, UNUM32 hCLL, CALLBACKFNC cb)
{
	ENSURE(RegisterEventCallback)
	T_PDU_ERROR e = real.RegisterEventCallback(hMod, hCLL, cb);
	logf("PDURegisterEventCallback(hMod=%lu, hCLL=%lu, cb=%p) = 0x%lX", (unsigned long)hMod,
		(unsigned long)hCLL, (void *)cb, (unsigned long)e);
	return e;
}

T_PDU_ERROR API PDUGetResourceIds(UNUM32 hMod, PDU_RSC_DATA *rsc, PDU_RSC_ID_ITEM **list)
{
	ENSURE(GetResourceIds)
	T_PDU_ERROR e = real.GetResourceIds(hMod, rsc, list);
	logf("PDUGetResourceIds(hMod=%lu, bus=%lu, prot=%lu) = 0x%lX", (unsigned long)hMod,
		rsc ? (unsigned long)rsc->BusTypeId : 0UL, rsc ? (unsigned long)rsc->ProtocolId : 0UL, (unsigned long)e);
	if (e == PDU_STATUS_NOERROR && list && *list && (*list)->pResourceIdDataArray) {
		for (UNUM32 i = 0; i < (*list)->NumModules; i++) {
			PDU_RSC_ID_ITEM_DATA *d = &(*list)->pResourceIdDataArray[i];
			for (UNUM32 k = 0; isMasked(d->hMod) && d->pResourceIdArray && k < d->NumIds; k++)
				d->pResourceIdArray[k] = rscToMasked(d->pResourceIdArray[k]);
			logf("  hMod=%lu: %lu resource(s)", (unsigned long)d->hMod, (unsigned long)d->NumIds);
		}
	}
	return e;
}

T_PDU_ERROR API PDUGetConflictingResources(UNUM32 rscId, PDU_MODULE_ITEM *in, PDU_RSC_CONFLICT_ITEM **out)
{
	ENSURE(GetConflictingResources)
	/* Masked ids are VAS6154 resources and never collide with real J2534 ids. */
	T_PDU_ERROR e = real.GetConflictingResources(rscToReal(rscId), in, out);
	if (e == PDU_STATUS_NOERROR && out && *out && (*out)->pRscConflictData)
		for (UNUM32 i = 0; i < (*out)->NumEntries; i++)
			if (isMasked((*out)->pRscConflictData[i].hMod))
				(*out)->pRscConflictData[i].ResourceId = rscToMasked((*out)->pRscConflictData[i].ResourceId);
	return e;
}

T_PDU_ERROR API PDUGetUniqueRespIdTable(UNUM32 hMod, UNUM32 hCLL, PDU_UNIQUE_RESP_ID_TABLE_ITEM **t)
{
	ENSURE(GetUniqueRespIdTable)
	return real.GetUniqueRespIdTable(hMod, hCLL, t);
}

T_PDU_ERROR API PDUSetUniqueRespIdTable(UNUM32 hMod, UNUM32 hCLL, PDU_UNIQUE_RESP_ID_TABLE_ITEM *t)
{
	ENSURE(SetUniqueRespIdTable)
	return real.SetUniqueRespIdTable(hMod, hCLL, t);
}
