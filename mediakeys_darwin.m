#import <Cocoa/Cocoa.h>
#include <IOKit/hidsystem/ev_keymap.h>
#include "_cgo_export.h"

void startMediaKeyMonitor(void) {
    [NSEvent addGlobalMonitorForEventsMatchingMask:NSEventMaskSystemDefined
                                           handler:^(NSEvent *event) {
        // Subtype 8 == NX_SUBTYPE_AUX_CONTROL_BUTTONS (media keys)
        if ([event subtype] != 8) {
            return;
        }
        int keyCode  = (([event data1] & 0xFFFF0000) >> 16);
        int keyFlags = ( [event data1] & 0x0000FFFF);
        BOOL keyDown = ((keyFlags & 0xFF00) >> 8) == 0xA;
        if (keyDown) {
            goMediaKeyCallback(keyCode);
        }
    }];

    // Keep this thread's run loop alive so the monitor keeps firing.
    [[NSRunLoop currentRunLoop] run];
}
