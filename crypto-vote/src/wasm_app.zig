const std = @import("std");
const builtin = @import("builtin");
const App = @import("App.zig");
const CallbackInterface = @import("CallbackInterface.zig");

pub const std_options = std.Options{
    .cryptoRandomSeed = getRandom,
};

pub const allocator = if (builtin.is_test) std.testing.allocator else std.heap.wasm_allocator;

export fn alloc(size: u32) ?[*]u8 {
    const buf = allocator.alloc(u8, size) catch return null;
    return buf.ptr;
}

export fn free(ptr: [*]u8, size: u32) void {
    allocator.free(ptr[0..size]);
}

const Env = struct {
    extern fn console_log(ptr: [*]const u8, len: usize) void;
    extern fn get_random(ptr: [*]u8, amount: u32) void;
    extern fn publish_key_public(ptr: *const [CallbackInterface.publicKeySize]u8) i32;
    extern fn publish_vote(ptr: [*]const u8, len: usize) i32;
    extern fn set_can_vote(size: u32) void; // 0 means, can not vote.
};

const wasm_callbacks = CallbackInterface{
    .publishKeyPublicFn = publishKeyPublic,
    .publishVoteFn = publishVote,
    .setCanVoteFn = setCanVote,
    .logFn = consoleLog,
};

fn consoleLog(msg: []const u8) void {
    Env.console_log(msg.ptr, msg.len);
}

fn getRandom(buf: []u8) void {
    Env.get_random(buf.ptr, buf.len);
}

fn publishKeyPublic(key: [CallbackInterface.publicKeySize]u8) CallbackInterface.CallbackError!void {
    const result = Env.publish_key_public(&key);
    if (result != 0) {
        return CallbackInterface.fromErrorCode(@enumFromInt(result));
    }
}

fn publishVote(vote: []const u8) CallbackInterface.CallbackError!void {
    const result = Env.publish_vote(vote.ptr, vote.len);
    if (result != 0) {
        return CallbackInterface.fromErrorCode(@enumFromInt(result));
    }
}

fn setCanVote(size: ?u32) void {
    Env.set_can_vote(size orelse 0);
}

fn readBufWithSize(buf: [*]const u8) []const u8 {
    const size = std.mem.readInt(u32, buf[0..4], .little);
    return buf[4..][0..size];
}

var app: ?App = null;

export fn start(user_id: u32) void {
    app = App.init(&wasm_callbacks, user_id);
}

export fn onmessage(event_ptr: [*]const u8, event_len: usize) void {
    var loaded_app = app orelse {
        consoleLog("App not initialized");
        return;
    };

    loaded_app.processEvent(
        allocator,
        event_ptr[0..event_len],
    ) catch |err| {
        const msg = std.fmt.allocPrint(allocator, "Error processing event: {}", .{err}) catch "OOM";
        consoleLog(msg);
        return;
    };
}

export fn encrypt_and_send_vote(vote_ptr: [*]const u8, vote_len: usize) u32 {
    var loaded_app = app orelse {
        consoleLog("App not initialized");
        return 1;
    };

    const vote = vote_ptr[0..vote_len];
    loaded_app.encryptAndSendVote(allocator, vote) catch |err| {
        const msg = std.fmt.allocPrint(allocator, "Error encrypting and sending vote: {}", .{err}) catch "OOM";
        defer allocator.free(msg);
        consoleLog(msg);
        return 1;
    };

    return 0;
}
