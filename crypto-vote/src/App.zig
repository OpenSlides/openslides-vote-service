const std = @import("std");
const Allocator = std.mem.Allocator;
const event = @import("event.zig");
const crypto = @import("crypto.zig");
const CallbackInterface = @import("CallbackInterface.zig");

const App = @This();

callback: *const CallbackInterface,
user_id: u32,
last_hash: ?[32]u8 = null,
poll_worker_amount: u32 = 0,
can_i_vote: bool = false,
vote_size: u32 = 0,
key_pair_mixnet: ?crypto.KeyPairMixnet = null,
key_pair_trustee: ?crypto.KeyPairTrustee = null,

pub fn init(callback: *const CallbackInterface, user_id: u32) App {
    return .{
        .callback = callback,
        .user_id = user_id,
    };
}

pub fn processEvent(self: *App, allocator: Allocator, event_data: []const u8) !void {
    const parsedEvent = try event.parseEvent(allocator, event_data, self.last_hash);
    self.last_hash = event.hashEvent(event_data);

    switch (parsedEvent) {
        .start => |value| {
            self.vote_size = value.vote_size;
            self.poll_worker_amount = value.poll_worker_ids.len;
            if (std.mem.indexOfScalar(u32, value.vote_user_ids, self.user_id) != null) {
                self.can_i_vote = true;
            }
            if (std.mem.indexOfScalar(u32, value.poll_worker_ids, self.user_id) != null) {
                self.key_pair_mixnet = crypto.KeyPairMixnet.generate();
                self.key_pair_trustee = crypto.KeyPairTrustee.generate();

                var both_keys: [64]u8 = undefined;
                @memcpy(both_keys[0..32], &self.key_pair_mixnet.?.key_public);
                @memcpy(both_keys[32..64], &self.key_pair_trustee.?.key_public);
                const Encoder = std.base64.standard_no_pad.Encoder;
                var as_base64: [CallbackInterface.publicKeySize]u8 = undefined;
                _ = Encoder.encode(&as_base64, &both_keys);
                try self.callback.publishKeyPublic(as_base64);
            }
        },
        else => undefined, // TODO
    }
}
