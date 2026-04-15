package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework MediaPlayer
#include <stdlib.h>

void startMediaKeyMonitor(const char *title);
void setNowPlayingPlaybackState(int playing);
void RunMainLoop(void);
*/
import "C"
import "unsafe"

// globalMediaKeyCh is set once by startMediaKeyMonitor and read by the
// Objective-C callback dispatched on the main queue.
var globalMediaKeyCh chan Command

//export goMediaKeyCallback
func goMediaKeyCallback(keyCode C.int) {
	var cmd Command
	switch int(keyCode) {
	case 16: // NX_KEYTYPE_PLAY  — headphone pause/play button
		cmd = CmdPause
	case 17: // NX_KEYTYPE_NEXT
		cmd = CmdNext
	case 18: // NX_KEYTYPE_PREVIOUS
		cmd = CmdPrev
	default:
		return
	}
	select {
	case globalMediaKeyCh <- cmd:
	default:
	}
}

// UpdateNowPlayingState tells macOS whether this process is actively playing,
// which causes macOS to route media-key events here instead of Apple Music.
// Safe to call from any goroutine; dispatches to the main queue internally.
func UpdateNowPlayingState(playing bool) {
	v := C.int(0)
	if playing {
		v = 1
	}
	C.setNowPlayingPlaybackState(v)
}

// startMediaKeyMonitor schedules MPRemoteCommandCenter registration on the
// Cocoa main queue and forwards matching events to ch as Commands.
// Must be called before RunMainLoop().
func startMediaKeyMonitor(ch chan Command, title string) {
	globalMediaKeyCh = ch
	cs := C.CString(title)
	// cs is passed to ObjC which copies it into an NSString, so we can free
	// immediately after the call returns (dispatch_async has already captured it).
	// Actually, the block captures titleStr (NSString), not cs, so free is safe.
	C.startMediaKeyMonitor(cs)
	C.free(unsafe.Pointer(cs))
}

// runMainLoop runs the Cocoa main run loop on the current OS thread forever.
// The caller must have locked its goroutine to the OS main thread via
// runtime.LockOSThread() before calling this.
func runMainLoop() {
	C.RunMainLoop()
}
