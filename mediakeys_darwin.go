package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework MediaPlayer

void startMediaKeyMonitor(void);
*/
import "C"
import "runtime"

// globalMediaKeyCh is set once by startMediaKeyMonitor and read by the
// Objective-C callback on its own OS thread, so no synchronisation is needed
// beyond the happens-before guarantee of the goroutine start.
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

// startMediaKeyMonitor registers a global NSEvent monitor for media keys and
// forwards matching key-down events to ch as Commands.
// It must be called before Player.Run().
//
// NOTE: macOS requires "Input Monitoring" access (System Settings →
// Privacy & Security → Input Monitoring) for global monitors to receive
// events from other applications.
func startMediaKeyMonitor(ch chan Command) {
	globalMediaKeyCh = ch
	go func() {
		runtime.LockOSThread() // NSRunLoop must stay on one OS thread
		C.startMediaKeyMonitor()
	}()
}
