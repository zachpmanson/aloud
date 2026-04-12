#import <Cocoa/Cocoa.h>
#import <MediaPlayer/MediaPlayer.h>
#include "_cgo_export.h"

void startMediaKeyMonitor(void) {
    [NSApplication sharedApplication];

    // Register minimal now-playing info so macOS routes media key commands
    // to this process instead of (or in addition to) other media apps.
    [[MPNowPlayingInfoCenter defaultCenter] setNowPlayingInfo:@{
        MPNowPlayingInfoPropertyMediaType: @(MPNowPlayingInfoMediaTypeAudio),
    }];

    MPRemoteCommandCenter *center = [MPRemoteCommandCenter sharedCommandCenter];

    // play, pause, and toggle all map to the same pause/resume command.
    [center.playCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
        goMediaKeyCallback(16); // NX_KEYTYPE_PLAY
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
        goMediaKeyCallback(17); // NX_KEYTYPE_NEXT
        return MPRemoteCommandHandlerStatusSuccess;
    }];
    [center.previousTrackCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
        goMediaKeyCallback(18); // NX_KEYTYPE_PREVIOUS
        return MPRemoteCommandHandlerStatusSuccess;
    }];

    // Keep this thread's run loop alive so the command center keeps firing.
    [[NSRunLoop currentRunLoop] run];
}
