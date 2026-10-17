//go:build linux && (amd64 || arm64)

package sandbox

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Driver libraries are supplied by the existing read-only host tools mounts.
// No host installation, device creation, CDI hooks or privileged daemon is used.
func nvidiaInventory(ctx context.Context) ([]platform.GPUDevice, map[string]gpuNode, error) {
	f, err := pin("/usr/bin/nvidia-smi", false)
	if err != nil {
		return nil, nil, unavailable("gpu_driver_unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode()&0022 != 0 {
		return nil, nil, unavailable("gpu_driver_unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/nvidia-smi", "-q", "-x")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	out, err := cmd.Output()
	if err != nil || len(out) > 4<<20 {
		return nil, nil, unavailable("gpu_driver_unavailable")
	}
	var inventory struct {
		GPUs []struct {
			UUID   string `xml:"uuid"`
			Name   string `xml:"product_name"`
			Minor  string `xml:"minor_number"`
			Memory string `xml:"fb_memory_usage>total"`
		} `xml:"gpu"`
	}
	if xml.Unmarshal(out, &inventory) != nil {
		return nil, nil, unavailable("gpu_inventory_invalid")
	}
	var data bytes.Buffer
	writer := csv.NewWriter(&data)
	for _, g := range inventory.GPUs {
		memory := strings.TrimSuffix(g.Memory, " MiB")
		_ = writer.Write([]string{g.UUID, g.Name, memory, g.Minor})
	}
	writer.Flush()
	return parseNVIDIAInventory(data.Bytes())
}
func parseNVIDIAInventory(data []byte) ([]platform.GPUDevice, map[string]gpuNode, error) {
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) > 32 {
		return nil, nil, unavailable("gpu_inventory_invalid")
	}
	devices := []platform.GPUDevice{}
	nodes := map[string]gpuNode{}
	for _, row := range rows {
		if len(row) != 4 {
			return nil, nil, unavailable("gpu_inventory_invalid")
		}
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		id := row[0]
		memory, e1 := strconv.ParseInt(row[2], 10, 64)
		minor, e2 := strconv.ParseUint(row[3], 10, 20)
		if core.ValidateGPUDevices([]string{id}) != nil || e1 != nil || e2 != nil || memory <= 0 || memory > 1<<30 || nodes[id].Path != "" {
			return nil, nil, unavailable("gpu_inventory_invalid")
		}
		nodes[id] = gpuNode{Path: fmt.Sprintf("/dev/nvidia%d", minor), Minor: uint32(minor)}
		devices = append(devices, platform.GPUDevice{ID: id, Name: row[1], Vendor: "nvidia", MemoryBytes: memory << 20})
	}
	return devices, nodes, nil
}
func (r *Runner) prepareGPU(ctx context.Context) {
	if !r.options.GPU.Enabled {
		return
	}
	for _, id := range r.options.GPU.Devices {
		r.gpuDevices = append(r.gpuDevices, platform.GPUDevice{ID: id, Vendor: "nvidia", Reason: "gpu_unavailable"})
	}
	fail := func(reason string) {
		for i := range r.gpuDevices {
			r.gpuDevices[i].Reason = reason
		}
	}
	if !r.options.HostTools {
		fail("gpu_host_tools_required")
		return
	}
	inventory, nodes, err := nvidiaInventory(ctx)
	if err != nil {
		fail(string(core.ReasonOf(err)))
		return
	}
	if r.options.GPU.AllDevices {
		r.gpuDevices = append([]platform.GPUDevice(nil), inventory...)
		r.options.GPU.Devices = nil
		for _, d := range inventory {
			r.options.GPU.Devices = append(r.options.GPU.Devices, d.ID)
		}
	}
	r.gpuNodes = nodes
	for i, d := range r.gpuDevices {
		idx := slices.IndexFunc(inventory, func(v platform.GPUDevice) bool { return v.ID == d.ID })
		if idx < 0 {
			r.gpuDevices[i].Reason = "gpu_device_missing"
			continue
		}
		r.gpuDevices[i] = inventory[idx]
		probe := platformProbe()
		probe.Limits.MemoryBytes = 1 << 30
		probe.Limits.Processes = 128
		probe.Timeout = 8 * time.Second
		probe.MaxOutputBytes = 8192
		if r.options.NoWorkspace {
			probe.Directory = "/tmp"
		}
		probe.GPUDeviceIDs = []string{d.ID}
		probe.Script = gpuProbeScript(probe.GPUDeviceIDs)
		p, err := r.startAuthorized(ctx, probeExecution(), probe, func(context.Context, core.Execution, platform.Command) error { return nil })
		if err != nil {
			r.gpuDevices[i].Reason = string(core.ReasonOf(err))
			if r.gpuDevices[i].Reason == "" {
				r.gpuDevices[i].Reason = "gpu_probe_failed"
			}
			continue
		}
		// Drain both streams; the probe never depends on a consumer reading output.
		_, _ = io.Copy(io.Discard, p.Stdout())
		_, _ = io.Copy(io.Discard, p.Stderr())
		result, err := p.Wait(ctx)
		if err != nil || result.Code != 0 {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = p.Cancel(cleanup)
			cancel()
			r.gpuDevices[i].Reason = "gpu_probe_failed"
			continue
		}
		r.gpuDevices[i].Available = true
		r.gpuDevices[i].Reason = "available"
	}
}
func (r *Runner) checkGPUAccess(c platform.Command) error {
	if len(c.GPUDeviceIDs) == 0 {
		return nil
	}
	if c.FullAccess {
		return core.Invalid("gpu", "Full Access does not accept an isolated GPU scope")
	}
	if !r.options.GPU.Enabled {
		return unavailable("gpu_disabled")
	}
	if err := core.CheckGPUDevices(c.GPUDeviceIDs, r.options.GPU.Devices); err != nil {
		return err
	}
	for _, id := range c.GPUDeviceIDs {
		i := slices.IndexFunc(r.gpuDevices, func(v platform.GPUDevice) bool { return v.ID == id })
		if i < 0 || !r.gpuDevices[i].Available {
			return unavailable("gpu_unavailable")
		}
	}
	return nil
}
func (r *Runner) appendGPUMounts(ctx context.Context, args *[]string, add func(*os.File) string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if !r.options.GPU.Enabled || !r.options.HostTools {
		return unavailable("gpu_unavailable")
	}
	if err := core.CheckGPUDevices(ids, r.options.GPU.Devices); err != nil {
		return err
	}
	_, current, err := nvidiaInventory(ctx)
	if err != nil {
		return err
	}
	paths := []string{"/dev/nvidiactl", "/dev/nvidia-uvm"}
	for _, id := range ids {
		original, exists := r.gpuNodes[id]
		now := current[id]
		if !exists || now != original {
			return unavailable("gpu_device_changed")
		}
		paths = append(paths, original.Path)
	}
	for _, path := range paths {
		f, err := pin(path, false)
		if err != nil {
			return unavailable("gpu_device_unavailable")
		}
		st, err := f.Stat()
		if err != nil || st.Mode()&os.ModeCharDevice == 0 {
			f.Close()
			return unavailable("gpu_device_invalid")
		}
		var stat unix.Stat_t
		if unix.Fstat(int(f.Fd()), &stat) != nil {
			f.Close()
			return unavailable("gpu_device_invalid")
		}
		if strings.HasPrefix(path, "/dev/nvidia") && path != "/dev/nvidiactl" && path != "/dev/nvidia-uvm" {
			minor, err := strconv.ParseUint(strings.TrimPrefix(path, "/dev/nvidia"), 10, 32)
			if err != nil || unix.Minor(uint64(stat.Rdev)) != uint32(minor) {
				f.Close()
				return unavailable("gpu_device_changed")
			}
		}
		// FD-pinned source prevents replacement between validation and mounting.
		*args = append(*args, "--dev-bind", "/proc/self/fd/"+add(f), path)
	}
	return nil
}

// CUDA Driver API is used without CUDA_VISIBLE_DEVICES. The probe enumerates
// the actual usable devices, creates a context and performs a tiny GPU memory
// operation. It fails if an unselected GPU can be reached through shared nodes.
func gpuProbeScript(ids []string) string {
	data, _ := json.Marshal(ids)
	return "/usr/bin/python3 - <<'MOLR_GPU_PROBE'\n" + strings.ReplaceAll(gpuProbePython, "EXPECTED", string(data)) + "\nMOLR_GPU_PROBE"
}

const gpuProbePython = `import ctypes as c, uuid, socket
server=socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET)
server.bind('\0molr-gpu-probe');server.listen(1)
client=socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET)
client.connect('\0molr-gpu-probe')
peer,_=server.accept();client.send(b'ok');assert peer.recv(2)==b'ok'
peer.close();client.close();server.close()
lib=c.CDLL('libcuda.so.1')
def check(result):
    if result != 0: raise RuntimeError('CUDA error '+str(result))
check(lib.cuInit(0))
count=c.c_int(); check(lib.cuDeviceGetCount(c.byref(count)))
found=[]
for index in range(count.value):
    device=c.c_int(); check(lib.cuDeviceGet(c.byref(device),index))
    raw=(c.c_ubyte*16)(); check(lib.cuDeviceGetUuid(c.byref(raw),device))
    identity='GPU-'+str(uuid.UUID(bytes=bytes(raw)))
    context=c.c_void_p()
    check(lib.cuCtxCreate_v2(c.byref(context),0,device))
    found.append(identity)
    pointer=c.c_uint64()
    check(lib.cuMemAlloc_v2(c.byref(pointer),c.c_size_t(4096)))
    check(lib.cuMemsetD8_v2(pointer,c.c_ubyte(42),c.c_size_t(4096)))
    output=(c.c_ubyte*4096)()
    check(lib.cuMemcpyDtoH_v2(output,pointer,c.c_size_t(4096)))
    assert all(value==42 for value in output)
    check(lib.cuMemFree_v2(pointer)); check(lib.cuCtxDestroy_v2(context))
assert sorted(found)==sorted(EXPECTED), found
print('gpu-probe-ok')`
