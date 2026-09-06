// scan_darwin.c implements the syscall-level half of Task 6's process
// scanner: sysctl(KERN_PROC_ALL) enumeration, proc_pidinfo/proc_pid_rusage
// per-pid reads, and KERN_PROCARGS2 argv extraction. All of it is
// passwordless for the calling user's own processes; calls against another
// user's pid fail with EPERM/ESRCH and the Go side treats that as "skip",
// never a crash.
//
// No Go-side declarations are duplicated here; the prototypes matching
// these definitions live in scan_darwin.go's cgo preamble.

#include <sys/sysctl.h>
#include <sys/types.h>
#include <sys/proc.h>
#include <sys/proc_info.h>
#include <libproc.h>
#include <stdlib.h>
#include <string.h>

// wattop_list_pids enumerates every pid on the system via
// sysctl(KERN_PROC_ALL). *out_pids is malloc'd by this function; the caller
// must free it with wattop_free. Returns 0 on success, -1 on error.
int wattop_list_pids(pid_t **out_pids, int *out_count) {
    int mib[3] = { CTL_KERN, KERN_PROC, KERN_PROC_ALL };
    size_t size = 0;

    if (sysctl(mib, 3, NULL, &size, NULL, 0) < 0) {
        return -1;
    }
    // The process table can grow between the sizing call and the fetch;
    // pad generously and retry once if we still come up short.
    size += size / 8 + 4096;

    struct kinfo_proc *procs = malloc(size);
    if (procs == NULL) {
        return -1;
    }
    if (sysctl(mib, 3, procs, &size, NULL, 0) < 0) {
        free(procs);
        return -1;
    }

    int n = (int)(size / sizeof(struct kinfo_proc));
    pid_t *pids = malloc(sizeof(pid_t) * (n > 0 ? n : 1));
    if (pids == NULL) {
        free(procs);
        return -1;
    }
    for (int i = 0; i < n; i++) {
        pids[i] = procs[i].kp_proc.p_pid;
    }
    free(procs);

    *out_pids = pids;
    *out_count = n;
    return 0;
}

// wattop_free releases memory allocated by wattop_list_pids.
void wattop_free(void *p) {
    free(p);
}

// wattop_task_info fills rss_bytes and cpu_ticks via
// proc_pidinfo(PROC_PIDTASKINFO). Returns 0 on success, -1 when the pid is
// gone or unreadable (e.g. another user's process).
//
// cpu_ticks is cumulative user+system CPU time in MACH ABSOLUTE TICKS, not
// nanoseconds: the kernel fills pti_total_user/pti_total_system from
// task_absolutetime_info, whose units are Mach absolute time despite the
// bare "total_user" naming. On Apple Silicon a tick is 125/3 ns, so a
// caller that reads these as nanoseconds understates CPU by ~41.7x — a
// `yes` process pinned to one core read 2.39% that way. The Go side runs
// this value through proc.MachTicksToNs before any percentage arithmetic;
// the parameter is named cpu_ticks so the units travel with it.
int wattop_task_info(pid_t pid, uint64_t *rss_bytes, uint64_t *cpu_ticks) {
    struct proc_taskinfo ti;
    int n = proc_pidinfo(pid, PROC_PIDTASKINFO, 0, &ti, sizeof(ti));
    if (n != (int)sizeof(ti)) {
        return -1;
    }
    *rss_bytes = ti.pti_resident_size;
    *cpu_ticks = ti.pti_total_user + ti.pti_total_system;
    return 0;
}

// wattop_comm copies up to buflen-1 bytes of p_comm (truncated, display-only
// — see the join-key comment in scan_darwin.go) into buf via
// proc_pidinfo(PROC_PIDT_SHORTBSDINFO). Returns 0 on success, -1 otherwise.
int wattop_comm(pid_t pid, char *buf, int buflen) {
    struct proc_bsdshortinfo bsi;
    int n = proc_pidinfo(pid, PROC_PIDT_SHORTBSDINFO, 0, &bsi, sizeof(bsi));
    if (n != (int)sizeof(bsi)) {
        return -1;
    }
    strncpy(buf, bsi.pbsi_comm, buflen - 1);
    buf[buflen - 1] = '\0';
    return 0;
}

// wattop_rusage fills disk I/O byte counters and the Mach start-abstime via
// proc_pid_rusage(RUSAGE_INFO_V4). Returns 0 on success, -1 on permission
// error or missing pid — callers must treat that as "leave these fields
// zero" rather than dropping the row.
int wattop_rusage(pid_t pid, uint64_t *diskread, uint64_t *diskwrite, uint64_t *start_abstime) {
    struct rusage_info_v4 ru;
    int rc = proc_pid_rusage(pid, RUSAGE_INFO_V4, (rusage_info_t *)&ru);
    if (rc != 0) {
        return -1;
    }
    *diskread = ru.ri_diskio_bytesread;
    *diskwrite = ru.ri_diskio_byteswritten;
    *start_abstime = ru.ri_proc_start_abstime;
    return 0;
}

// wattop_cwd copies the process's current working directory into buf via
// proc_pidinfo(PROC_PIDVNODEPATHINFO). Returns 0 on success, -1 on
// permission error or missing pid — best-effort, per the spec.
int wattop_cwd(pid_t pid, char *buf, int buflen) {
    struct proc_vnodepathinfo vpi;
    int n = proc_pidinfo(pid, PROC_PIDVNODEPATHINFO, 0, &vpi, sizeof(vpi));
    if (n != (int)sizeof(vpi)) {
        return -1;
    }
    strncpy(buf, vpi.pvi_cdir.vip_path, buflen - 1);
    buf[buflen - 1] = '\0';
    return 0;
}

// wattop_argv reads the raw KERN_PROCARGS2 blob for pid into buf (caller-
// owned, bufcap bytes). *out_size receives the number of valid bytes and
// *out_argc the argc encoded at the head of the blob; the Go side parses the
// NUL-separated exec path and argv strings out of it (that part has no
// syscalls and belongs on the Go side, not here). Returns 0 on success, -1
// on permission error or missing pid.
int wattop_argv(pid_t pid, char *buf, int bufcap, int *out_size, int *out_argc) {
    int mib[3] = { CTL_KERN, KERN_PROCARGS2, pid };
    size_t size = (size_t)bufcap;

    if (sysctl(mib, 3, buf, &size, NULL, 0) != 0) {
        return -1;
    }
    if (size < sizeof(int)) {
        return -1;
    }
    int argc = 0;
    memcpy(&argc, buf, sizeof(int));
    *out_argc = argc;
    *out_size = (int)size;
    return 0;
}
