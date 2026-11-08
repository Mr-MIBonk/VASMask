/*
 * Minimal ISO 22900-2 (D-PDU API) declarations used by the VAS6154 mask shim.
 *
 * Written for this project from the interface described by ISO 22900-2; only
 * the types, constants and structure layouts the shim touches are declared.
 * Enumerated types are plain 32-bit integers, which is how the D-PDU API
 * passes them on Windows (x86 and x64).
 */
#ifndef VAS6154MASK_PDU_API_H
#define VAS6154MASK_PDU_API_H

typedef unsigned char  UNUM8;
typedef signed char    SNUM8;
typedef unsigned short UNUM16;
typedef signed short   SNUM16;
typedef unsigned long  UNUM32; /* 32 bits on Windows (LLP64) */
typedef signed long    SNUM32;
typedef char           CHAR8;

typedef UNUM32 T_PDU_ERROR;
typedef UNUM32 T_PDU_IT;
typedef UNUM32 T_PDU_OBJT;
typedef UNUM32 T_PDU_STATUS;
typedef UNUM32 T_PDU_ERR_EVT;
typedef UNUM32 T_PDU_EVT_DATA;

/* Function results */
#define PDU_STATUS_NOERROR 0x00000000
#define PDU_ERR_FCT_FAILED 0x00000001

/* Item types */
#define PDU_IT_IO_UNUM32 0x1000
#define PDU_IT_RESULT    0x1300
#define PDU_IT_ERROR     0x1302

/* Object types for PDUGetObjectId */
#define PDU_OBJT_IO_CTRL 0x8023

#define PDU_ID_UNDEF 0xFFFFFFFE

typedef struct {
	T_PDU_IT ItemType;
} PDU_ITEM;

typedef struct {
	T_PDU_IT ItemType;
	void *pData;
} PDU_DATA_ITEM;

typedef struct {
	UNUM32 hMod;
	UNUM32 ResourceId;
	UNUM32 ResourceStatus;
} PDU_RSC_STATUS_DATA;

typedef struct {
	T_PDU_IT ItemType;
	UNUM32 NumEntries;
	PDU_RSC_STATUS_DATA *pResourceStatusData;
} PDU_RSC_STATUS_ITEM;

typedef struct {
	T_PDU_IT ItemType;
	UNUM32 ComParamId;
	UNUM32 ComParamDataType;
	UNUM32 ComParamClass;
	void *pComParamData;
} PDU_PARAM_ITEM;

typedef struct {
	UNUM32 ModuleTypeId;
	UNUM32 hMod;
	CHAR8 *pVendorModuleName;
	CHAR8 *pVendorAdditionalInfo;
	T_PDU_STATUS ModuleStatus;
} PDU_MODULE_DATA;

typedef struct {
	T_PDU_IT ItemType;
	UNUM32 NumEntries;
	PDU_MODULE_DATA *pModuleData;
} PDU_MODULE_ITEM;

typedef struct {
	UNUM32 hMod;
	UNUM32 NumIds;
	UNUM32 *pResourceIdArray;
} PDU_RSC_ID_ITEM_DATA;

typedef struct {
	T_PDU_IT ItemType;
	UNUM32 NumModules;
	PDU_RSC_ID_ITEM_DATA *pResourceIdDataArray;
} PDU_RSC_ID_ITEM;

typedef struct {
	UNUM32 DLCPinNumber;
	UNUM32 DLCPinTypeId;
} PDU_PIN_DATA;

typedef struct {
	UNUM32 BusTypeId;
	UNUM32 ProtocolId;
	UNUM32 NumPinData;
	PDU_PIN_DATA *pDLCPinData;
} PDU_RSC_DATA;

typedef struct {
	UNUM32 hMod;
	UNUM32 ResourceId;
} PDU_RSC_CONFLICT_DATA;

typedef struct {
	T_PDU_IT ItemType;
	UNUM32 NumEntries;
	PDU_RSC_CONFLICT_DATA *pRscConflictData;
} PDU_RSC_CONFLICT_ITEM;

typedef struct {
	T_PDU_IT ItemType;
	UNUM32 hCop;
	void *pCoPTag;
	UNUM32 Timestamp;
	void *pData;
} PDU_EVENT_ITEM;

typedef struct {
	T_PDU_ERR_EVT ErrorCodeId;
	UNUM32 ExtraErrorInfoId;
} PDU_ERROR_DATA;

typedef struct {
	UNUM32 NumFlagBytes;
	UNUM8 *pFlagData;
} PDU_FLAG_DATA;

typedef struct {
	UNUM32 NumHeaderBytes;
	UNUM32 NumFooterBytes;
	UNUM8 *pHeaderBytes;
	UNUM8 *pFooterBytes;
} PDU_EXTRA_INFO;

typedef struct {
	PDU_FLAG_DATA RxFlag;
	UNUM32 UniqueRespIdentifier;
	UNUM32 AcceptanceId;
	PDU_FLAG_DATA TimestampFlags;
	UNUM32 TxMsgDoneTimestamp;
	UNUM32 StartMsgTimestamp;
	PDU_EXTRA_INFO *pExtraInfo;
	UNUM32 NumDataBytes;
	UNUM8 *pDataBytes;
} PDU_RESULT_DATA;

typedef struct {
	UNUM32 MVCI_Part1StandardVersion;
	UNUM32 MVCI_Part2StandardVersion;
	UNUM32 HwSerialNumber;
	CHAR8 HwName[64];
	UNUM32 HwVersion;
	UNUM32 HwDate;
	UNUM32 HwInterface;
	CHAR8 FwName[64];
	UNUM32 FwVersion;
	UNUM32 FwDate;
	CHAR8 VendorName[64];
	CHAR8 PDUApiSwName[64];
	UNUM32 PDUApiSwVersion;
	UNUM32 PDUApiSwDate;
} PDU_VERSION_DATA;

/* Only passed through by the shim. */
typedef struct PDU_COP_CTRL_DATA PDU_COP_CTRL_DATA;
typedef struct PDU_UNIQUE_RESP_ID_TABLE_ITEM PDU_UNIQUE_RESP_ID_TABLE_ITEM;

typedef void (__stdcall *CALLBACKFNC)(T_PDU_EVT_DATA eventType, UNUM32 hMod, UNUM32 hCLL, void *pCllTag, void *pAPITag);

#endif
