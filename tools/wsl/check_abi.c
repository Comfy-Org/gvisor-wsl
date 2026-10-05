// Copyright 2026 The gVisor Authors.
// SPDX-License-Identifier: Apache-2.0
// Compile with -I <WSL2-Linux-Kernel>/include/uapi.
#include <linux/types.h>
#include <stddef.h>
#include <sys/ioctl.h>
#include <misc/d3dkmthk.h>

#define IO(name, value) _Static_assert(name == value, #name)
#define OFF(type, member, value) _Static_assert(offsetof(struct type, member) == value, #type "." #member)
IO(LX_DXOPENADAPTERFROMLUID, 0xc00c4701);
IO(LX_DXCREATEDEVICE, 0xc0404702);
IO(LX_DXCREATECONTEXTVIRTUAL, 0xc0284704);
IO(LX_DXDESTROYCONTEXT, 0xc0044705);
IO(LX_DXCREATEALLOCATION, 0xc0484706);
IO(LX_DXCREATEPAGINGQUEUE, 0xc0204707);
IO(LX_DXRESERVEGPUVIRTUALADDRESS, 0xc0484708);
IO(LX_DXQUERYADAPTERINFO, 0xc0184709);
IO(LX_DXQUERYVIDEOMEMORYINFO, 0xc038470a);
IO(LX_DXMAKERESIDENT, 0xc030470b);
IO(LX_DXMAPGPUVIRTUALADDRESS, 0xc068470c);
IO(LX_DXESCAPE, 0xc020470d);
IO(LX_DXCREATESYNCHRONIZATIONOBJECT, 0xc0604710);
IO(LX_DXDESTROYALLOCATION2, 0xc0184713);
IO(LX_DXCLOSEADAPTER, 0xc0044715);
IO(LX_DXCREATEHWQUEUE, 0xc0304718);
IO(LX_DXDESTROYDEVICE, 0xc0044719);
IO(LX_DXDESTROYHWQUEUE, 0xc004471b);
IO(LX_DXDESTROYPAGINGQUEUE, 0xc004471c);
IO(LX_DXDESTROYSYNCHRONIZATIONOBJECT, 0xc004471d);
IO(LX_DXFREEGPUVIRTUALADDRESS, 0xc0184720);
IO(LX_DXSUBMITCOMMANDTOHWQUEUE, 0xc0384734);
IO(LX_DXSUBMITWAITFORSYNCOBJECTSTOHWQUEUE, 0xc0184736);
IO(LX_DXENUMADAPTERS3, 0xc018473e);
IO(LX_DXSHAREOBJECTS, 0xc028473f);
IO(LX_DXQUERYRESOURCEINFOFROMNTHANDLE, 0xc0284741);
IO(LX_DXOPENRESOURCEFROMNTHANDLE, 0xc0684742);
OFF(d3dkmt_shareobjects, objects, 8);
OFF(d3dkmt_shareobjects, shared_handle, 32);
OFF(d3dkmt_queryresourceinfofromnthandle, nt_handle, 8);
OFF(d3dkmt_openresourcefromnthandle, nt_handle, 8);
OFF(d3dkmt_openresourcefromnthandle, allocation_count, 16);
OFF(d3dkmt_openresourcefromnthandle, open_alloc_info, 24);
OFF(d3dkmt_openresourcefromnthandle, private_runtime_data_size, 32);
OFF(d3dkmt_openresourcefromnthandle, private_runtime_data, 40);
OFF(d3dkmt_openresourcefromnthandle, resource_priv_drv_data_size, 48);
OFF(d3dkmt_openresourcefromnthandle, resource_priv_drv_data, 56);
OFF(d3dkmt_openresourcefromnthandle, total_priv_drv_data_size, 64);
OFF(d3dkmt_openresourcefromnthandle, total_priv_drv_data, 72);
OFF(d3dkmt_openresourcefromnthandle, keyed_mutex_private_data, 88);
OFF(d3dkmt_openresourcefromnthandle, keyed_mutex_private_data_size, 96);
_Static_assert(sizeof(struct d3dddi_openallocationinfo2) == 80, "openallocationinfo2");
_Static_assert(sizeof(void *) == 8, "64-bit ABI required");
_Static_assert(sizeof(struct d3dkmt_queryvideomemoryinfo) == 56, "queryvideomemoryinfo");
OFF(d3dkmt_queryvideomemoryinfo, process, 0);
OFF(d3dkmt_queryvideomemoryinfo, budget, 16);
OFF(d3dkmt_queryvideomemoryinfo, physical_adapter_index, 48);
_Static_assert(sizeof(struct d3dddi_allocationinfo2) == 96, "allocationinfo2");
_Static_assert(sizeof(struct d3dkmt_adapterinfo) == 20, "adapterinfo");
_Static_assert(sizeof(struct d3dkmt_createstandardallocation) == 24, "standardallocation");
OFF(d3dkmt_enumadapters3, adapter_count, 8);
OFF(d3dkmt_enumadapters3, adapters, 16);
OFF(d3dkmt_createallocation, private_runtime_data, 16);
OFF(d3dkmt_createallocation, private_runtime_data_size, 24);
OFF(d3dkmt_createallocation, priv_drv_data, 32);
OFF(d3dkmt_createallocation, priv_drv_data_size, 40);
OFF(d3dkmt_createallocation, alloc_count, 44);
OFF(d3dkmt_createallocation, allocation_info, 48);
OFF(d3dkmt_createallocation, flags, 56);
OFF(d3dddi_allocationinfo2, sysmem, 8);
OFF(d3dddi_allocationinfo2, priv_drv_data, 16);
OFF(d3dddi_allocationinfo2, priv_drv_data_size, 24);
OFF(d3dkmt_createpagingqueue, fence_cpu_virtual_address, 16);
OFF(d3dkmt_createhwqueue, priv_drv_data_size, 8);
OFF(d3dkmt_createhwqueue, priv_drv_data, 16);
OFF(d3dkmt_createhwqueue, queue_progress_fence_cpu_va, 32);
OFF(d3dkmt_createsynchronizationobject2, info.type, 8);
OFF(d3dkmt_createsynchronizationobject2, info.monitored_fence.fence_cpu_virtual_address, 24);
OFF(d3dkmt_createsynchronizationobject2, info.periodic_monitored_fence.fence_cpu_virtual_address, 32);
int main(void) { return 0; }
