//go:build darwin && arm64 && cgo

package soc

/*
// -lIOReport is deliberately absent here: mactop/ioreport.go already passes
// it, cgo link flags apply to the whole binary, and repeating it only earns
// an "ld: warning: ignoring duplicate libraries" on every build. Package soc
// links mactop on this build tag either way (see shim.go), so the symbols
// below always resolve.
#cgo LDFLAGS: -framework CoreFoundation -framework IOKit
#include <CoreFoundation/CoreFoundation.h>
#include <stdint.h>
#include <string.h>

extern CFDictionaryRef IOReportCopyChannelsInGroup(CFStringRef group, CFStringRef subgroup, uint64_t a, uint64_t b, uint64_t c);
extern CFStringRef IOReportChannelGetGroup(CFDictionaryRef item);

typedef struct {
	char name[64];
	int  channels;
} wattop_ioreport_group_t;

// wattop_tally_group walks one IOReportCopyChannelsInGroup result --
// a dictionary whose "IOReportChannels" key holds an array of per-channel
// dictionaries -- and folds each channel into out[] by its group name.
static void wattop_tally_group(CFDictionaryRef chans, wattop_ioreport_group_t *out, int max, int *n) {
	if (chans == NULL) {
		return;
	}
	CFArrayRef arr = (CFArrayRef)CFDictionaryGetValue(chans, CFSTR("IOReportChannels"));
	if (arr == NULL) {
		return;
	}
	CFIndex count = CFArrayGetCount(arr);
	for (CFIndex i = 0; i < count; i++) {
		CFDictionaryRef item = (CFDictionaryRef)CFArrayGetValueAtIndex(arr, i);
		if (item == NULL) {
			continue;
		}
		char group[64] = {0};
		CFStringRef groupRef = IOReportChannelGetGroup(item);
		if (groupRef != NULL) {
			CFStringGetCString(groupRef, group, sizeof(group), kCFStringEncodingUTF8);
		}
		if (group[0] == '\0') {
			strncpy(group, "(unnamed)", sizeof(group) - 1);
		}
		int at = -1;
		for (int j = 0; j < *n; j++) {
			if (strcmp(out[j].name, group) == 0) {
				at = j;
				break;
			}
		}
		if (at < 0) {
			if (*n >= max) {
				continue;
			}
			at = (*n)++;
			memset(&out[at], 0, sizeof(out[at]));
			strncpy(out[at].name, group, sizeof(out[at].name) - 1);
		}
		out[at].channels++;
	}
}

// wattopIOReportGroups fills out[] with one entry per distinct group name
// and returns how many it wrote, setting *wildcard to 1 if the listing came
// from the wildcard channel copy (every group the machine publishes) or 0
// if it came from the named-group fallback (only the groups listed below).
// The wildcard copy returns NULL on some OS versions -- including macOS 27
// on M5 Max, measured 2026-09-06 -- which is why mactop's own debugIOReport
// carries the fallback and why this does too.
static int wattopIOReportGroups(wattop_ioreport_group_t *out, int max, int *wildcard) {
	int n = 0;

	*wildcard = 0;
	CFDictionaryRef all = IOReportCopyChannelsInGroup(NULL, NULL, 0, 0, 0);
	if (all != NULL) {
		wattop_tally_group(all, out, max, &n);
		CFRelease(all);
	}
	if (n > 0) {
		*wildcard = 1;
		return n;
	}

	static const char *named[] = {
		"Energy Model", "Energy Counters", "GPU Stats", "CPU Stats",
		"AMC Stats", "PMP", "CLPC", "ODS", "Performance Statistics", NULL,
	};
	for (int i = 0; named[i] != NULL; i++) {
		CFStringRef g = CFStringCreateWithCString(kCFAllocatorDefault, named[i], kCFStringEncodingUTF8);
		if (g == NULL) {
			continue;
		}
		CFDictionaryRef ch = IOReportCopyChannelsInGroup(g, NULL, 0, 0, 0);
		if (ch != NULL) {
			wattop_tally_group(ch, out, max, &n);
			CFRelease(ch);
		}
		CFRelease(g);
	}
	return n;
}
*/
import "C"

import (
	"errors"
	"sort"
)

type Group struct {
	Name     string
	Channels int
}

const maxIOReportGroups = 128

var ErrNoIOReportGroups = errors.New("soc: IOReport reported no channel groups")

func IOReportGroups() (groups []Group, complete bool, err error) {
	buf := make([]C.wattop_ioreport_group_t, maxIOReportGroups)
	var wildcard C.int
	n := int(C.wattopIOReportGroups(&buf[0], C.int(maxIOReportGroups), &wildcard))
	if n <= 0 {
		return nil, false, ErrNoIOReportGroups
	}
	out := make([]Group, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Group{
			Name:     C.GoString(&buf[i].name[0]),
			Channels: int(buf[i].channels),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, wildcard != 0, nil
}
