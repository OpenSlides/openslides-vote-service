const std = @import("std");
const Allocator = std.mem.Allocator;
const Base64Encoder = std.base64.standard_no_pad.Encoder;
const event = @import("event.zig");
const crypto = @import("crypto.zig");
const CallbackInterface = @import("CallbackInterface.zig");

const App = @This();

callback: *const CallbackInterface,
user_id: u32,
last_hash: ?[32]u8 = null,
can_i_vote: bool = false,
vote_size: u32 = 0,
key_pair_mixnet: ?crypto.KeyPairMixnet = null,
key_pair_trustee: ?crypto.KeyPairTrustee = null,
poll_worker_ids: []u32 = &[_]u32{},
poll_worker_received: u32 = 0,
mixnet_key_public_list: [][32]u8 = &[_][32]u8{},
trustee_key_public_list: [][32]u8 = &[_][32]u8{},

pub fn init(callback: *const CallbackInterface, user_id: u32) App {
    return .{
        .callback = callback,
        .user_id = user_id,
    };
}

pub fn encryptAndSendVote(self: *App, allocator: Allocator, vote: []const u8) !void {
    if (!self.can_i_vote or self.mixnet_key_public_list.len < 2 or self.trustee_key_public_list.len < 2) {
        return error.VoteNotReady;
    }

    const encrypted_vote = try crypto.encrypt_message(
        allocator,
        self.mixnet_key_public_list,
        self.trustee_key_public_list,
        vote,
        self.vote_size,
    );
    defer encrypted_vote.free(allocator);

    const bs = try encrypted_vote.toBytes(allocator);
    defer allocator.free(bs);

    const as_base64 = try allocator.alloc(u8, Base64Encoder.calcSize(bs.len));
    defer allocator.free(as_base64);
    _ = Base64Encoder.encode(as_base64, bs);

    try self.callback.publishVote(as_base64);
    return;
}

pub fn processEvent(self: *App, allocator: Allocator, event_data: []const u8) !void {
    try self.callback.print(allocator, "event_data: {s}, last_hash: {any}", .{ event_data, self.last_hash });
    const parsedEvent = try event.parseEvent(allocator, event_data, self.last_hash);
    defer parsedEvent.free(allocator);
    self.callback.log("hier bin ich...");
    self.last_hash = event.hashEvent(event_data);

    switch (parsedEvent) {
        .start => |value| {
            self.vote_size = value.vote_size;
            self.poll_worker_ids = value.poll_worker_ids;
            self.mixnet_key_public_list = try allocator.alloc([32]u8, value.poll_worker_ids.len);
            self.trustee_key_public_list = try allocator.alloc([32]u8, value.poll_worker_ids.len);

            if (std.mem.indexOfScalar(u32, value.vote_user_ids, self.user_id) != null) {
                self.can_i_vote = true;
            }
            if (std.mem.indexOfScalar(u32, value.poll_worker_ids, self.user_id) == null) {
                return;
            }
            self.key_pair_mixnet = crypto.KeyPairMixnet.generate();
            self.key_pair_trustee = crypto.KeyPairTrustee.generate();

            var both_keys: [64]u8 = undefined;
            @memcpy(both_keys[0..32], &self.key_pair_mixnet.?.key_public);
            @memcpy(both_keys[32..64], &self.key_pair_trustee.?.key_public);

            var as_base64: [CallbackInterface.publicKeySize]u8 = undefined;
            _ = Base64Encoder.encode(&as_base64, &both_keys);
            // TODO: Try not to send key a second time. This will be complicated...
            try self.callback.publishKeyPublic(as_base64);
        },
        .publish_key_public => |value| {
            const poll_worker_index = std.mem.indexOfScalar(u32, self.poll_worker_ids, value.user_id) orelse {
                try self.callback.print(allocator, "public key from unauthorized poll_worker with id {}", .{value.user_id});
                return;
            };

            const mixnet_key = try allocator.dupe(u8, value.key_public[0..32]);
            const trustee_key = try allocator.dupe(u8, value.key_public[32..64]);

            self.mixnet_key_public_list[poll_worker_index] = mixnet_key[0..32].*;
            self.trustee_key_public_list[poll_worker_index] = trustee_key[0..32].*;
            self.poll_worker_received += 1;
            if (self.poll_worker_received != self.poll_worker_ids.len or !self.can_i_vote) {
                return;
            }

            self.callback.setCanVote(self.vote_size);
        },
        else => undefined, // TODO
    }
}
