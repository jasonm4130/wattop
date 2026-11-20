#include <sys/sysctl.h>
#include <sys/types.h>
#include <sys/proc.h>
#include <sys/proc_info.h>
#include <libproc.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <sys/time.h>

int wattop_list_pids(pid_t **out_pids, int64_t **out_start_usec, int *out_count) {
    int mib[3] = { CTL_KERN, KERN_PROC, KERN_PROC_ALL };
    size_t size = 0;

    if (sysctl(mib, 3, NULL, &size, NULL, 0) < 0) {
        return -1;
    }
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
    int64_t *starts = malloc(sizeof(int64_t) * (n > 0 ? n : 1));
    if (starts == NULL) {
        free(pids);
        free(procs);
        return -1;
    }
    for (int i = 0; i < n; i++) {
        pids[i] = procs[i].kp_proc.p_pid;
        struct timeval tv = procs[i].kp_proc.p_un.__p_starttime;
        starts[i] = (int64_t)tv.tv_sec * 1000000 + (int64_t)tv.tv_usec;
    }
    free(procs);

    *out_pids = pids;
    *out_start_usec = starts;
    *out_count = n;
    return 0;
}

void wattop_free(void *p) {
    free(p);
}

// pti_total_user and pti_total_system are Mach absolute ticks, not
// nanoseconds, despite the names.
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

int wattop_rusage(pid_t pid, uint64_t *diskread, uint64_t *diskwrite) {
    struct rusage_info_v4 ru;
    int rc = proc_pid_rusage(pid, RUSAGE_INFO_V4, (rusage_info_t *)&ru);
    if (rc != 0) {
        return -1;
    }
    *diskread = ru.ri_diskio_bytesread;
    *diskwrite = ru.ri_diskio_byteswritten;
    return 0;
}

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
