#import <Cocoa/Cocoa.h>
#import <MediaPlayer/MediaPlayer.h>
#include "_cgo_export.h"

// setNowPlayingPlaybackState is safe to call from any thread; it dispatches
// to the main queue where MPNowPlayingInfoCenter must be used.
void setNowPlayingPlaybackState(int playing) {
    MPNowPlayingPlaybackState state = playing
        ? MPNowPlayingPlaybackStatePlaying
        : MPNowPlayingPlaybackStatePaused;
    dispatch_async(dispatch_get_main_queue(), ^{
        [[MPNowPlayingInfoCenter defaultCenter] setPlaybackState:state];
    });
}

// startMediaKeyMonitor schedules Cocoa/MediaPlayer setup on the main queue.
// It must be called after RunMainLoop has started the main run loop (or at
// least after the main thread is locked and the run loop will be started
// imminently), so the dispatch lands on an active run loop.
void startMediaKeyMonitor(const char *title) {
    NSString *titleStr = title ? [NSString stringWithUTF8String:title] : @"aloud";
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApplication sharedApplication];

        [[MPNowPlayingInfoCenter defaultCenter] setNowPlayingInfo:@{
            MPNowPlayingInfoPropertyMediaType: @(MPNowPlayingInfoMediaTypeAudio),
            MPMediaItemPropertyTitle: titleStr,
        }];

        MPRemoteCommandCenter *center = [MPRemoteCommandCenter sharedCommandCenter];

        [center.playCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
            goMediaKeyCallback(16);
            return MPRemoteCommandHandlerStatusSuccess;
        }];
        [center.pauseCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
            goMediaKeyCallback(16);
            return MPRemoteCommandHandlerStatusSuccess;
        }];
        [center.togglePlayPauseCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
            goMediaKeyCallback(16);
            return MPRemoteCommandHandlerStatusSuccess;
        }];
        [center.nextTrackCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
            goMediaKeyCallback(17);
            return MPRemoteCommandHandlerStatusSuccess;
        }];
        [center.previousTrackCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
            goMediaKeyCallback(18);
            return MPRemoteCommandHandlerStatusSuccess;
        }];
    });
}

// RunMainLoop runs the Cocoa main run loop on the calling thread forever.
// Call this at the end of main() with the main goroutine locked to the OS
// main thread via runtime.LockOSThread().
void RunMainLoop(void) {
    [[NSRunLoop mainRunLoop] run];
}
