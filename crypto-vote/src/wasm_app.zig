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

const wasm_callbacks = CallbackInterface{
    .publishKeyPublicFn = publishKeyPublic,
    .logFn = consoleLog,
};

var app: ?App = null;

export fn start(user_id: u32) void {
    app = App.init(&wasm_callbacks, user_id);
}

export fn onmessage(event_ptr: [*]const u8, event_len: usize) void {
    var loaded_app = app orelse return;

    loaded_app.processEvent(
        allocator,
        event_ptr[0..event_len],
    ) catch |err| {
        const msg = std.fmt.allocPrint(allocator, "Error processing event: {}", .{err}) catch "OOM";
        consoleLog(msg);
        return;
    };
}
